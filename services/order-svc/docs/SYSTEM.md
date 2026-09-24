# TicketBottle Order Service - System Documentation

## Overview

**Purpose:** Saga Orchestrator for distributed ticket order transactions using Temporal workflows.

**Core Responsibility:** Coordinate order creation, payment processing, and inventory management across Event, Inventory, and Payment microservices with automatic compensation on failures.

**Key Technologies:** Go, Temporal Workflows, gRPC, Kafka, DynamoDB (AWS), JWT

---

## System Architecture

### High-Level Flow

```
User Request → gRPC API → Temporal CreateOrder Workflow
                        ↓
            [Reserve → Create → Pay]
                        ↓
            Payment Complete Event (Kafka)
                        ↓
            Temporal ConfirmOrder Workflow
                        ↓
            [Confirm Inventory → Update Status → Publish Event]
```

### Deployment Model

**Two Process Architecture:**

1. **API Server** (`cmd/api/main.go`)
   - Exposes gRPC endpoints for order operations
   - Runs Temporal worker for `create-order-tasks` queue
   - Executes CreateOrder workflow

2. **Consumer Server** (`cmd/consumer/main.go`)
   - Consumes payment events from Kafka
   - Runs Temporal worker for `confirm-order-tasks` queue
   - Executes ConfirmOrder workflow

---

## Project Structure

```
ticketbottle-order/
├── cmd/
│   ├── api/          # gRPC API server + CreateOrder worker
│   └── consumer/     # Kafka consumer + ConfirmOrder worker
├── config/           # Configuration management (env vars)
├── internal/
│   ├── activities/   # Temporal activities (Order, Inventory, Payment, Event)
│   ├── infra/        # Infrastructure setup (Kafka, DynamoDB, Temporal)
│   ├── interceptors/ # gRPC logging middleware
│   ├── models/       # Domain models (Order, OrderItem, CheckoutToken)
│   ├── order/        # Order module
│   │   ├── delivery/ # Delivery layer (gRPC, Kafka producer/consumer)
│   │   ├── repository/ # Data access layer (DynamoDB)
│   │   └── service/  # Business logic layer
│   └── workflows/    # Temporal workflow definitions
└── pkg/              # Reusable packages (jwt, logger, grpc clients, etc.)
```

---

## Data Models

### DynamoDB Single-Table Design

**Table Name:** `ticketbottle-orders`

| Entity | PK | SK | GSI1-PK | GSI1-SK | GSI2-PK | GSI2-SK |
|--------|----|----|---------|---------|---------|---------|
| Order | `ORDER#<code>` | `ORDER#<code>` | `USER#<userId>` | `ORDER#<createdAt>#<code>` | `EVENT#<eventId>` | `ORDER#<createdAt>#<code>` |
| OrderItem | `ORDER#<orderCode>` | `ITEM#<itemId>` | - | - | - | - |

**Access Patterns:**
- Get order by code: Query PK = `ORDER#<code>`, SK = `ORDER#<code>`
- Get order with items: Query PK = `ORDER#<code>` (returns order + all items)
- List orders by user: Query GSI1 PK = `USER#<userId>`, sorted by createdAt
- List orders by event: Query GSI2 PK = `EVENT#<eventId>`, sorted by createdAt
- Filter by status: FilterExpression on query results

### Order
```go
Order {
    // DynamoDB Keys
    PK           string              // Partition key: ORDER#<code>
    SK           string              // Sort key: ORDER#<code>
    GSI1PK       string              // GSI1 partition key: USER#<userId>
    GSI1SK       string              // GSI1 sort key: ORDER#<createdAt>#<code>
    GSI2PK       string              // GSI2 partition key: EVENT#<eventId>
    GSI2SK       string              // GSI2 sort key: ORDER#<createdAt>#<code>
    EntityType   string              // "ORDER"
    
    // Business Fields
    Code          string              // Unique order code (format: ORD-{timestamp}-{random}) - PRIMARY IDENTIFIER
    SessionID     string              // Optional waitroom session ID
    UserID        string
    UserFullName  string
    Email         string
    Phone         string
    EventID       string
    TotalAmount   int64              // Amount in cents
    Currency      string
    PaymentMethod PaymentMethod      // VNPAY, ZALOPAY, PAYOS
    Status        OrderStatus        // see Order Status Flow
    PaidAt        *time.Time
    CreatedAt     time.Time
    UpdatedAt     time.Time
    DeletedAt     *time.Time         // Soft delete support
}
```

### Order Item
```go
OrderItem {
    // DynamoDB Keys
    PK           string              // Partition key: ORDER#<orderCode>
    SK           string              // Sort key: ITEM#<itemId>
    EntityType   string              // "ORDER_ITEM"
    
    // Business Fields
    ID              string
    OrderCode       string
    TicketClassID   string
    TicketClassName string
    PriceAtPurchase int64              // Price snapshot at purchase time
    Quantity        int32
    TotalAmount     int64
    CreatedAt       time.Time
    UpdatedAt       time.Time
    DeletedAt       *time.Time
}
```

### Order Status Flow
```
PENDING → COMPLETED         ConfirmOrder, once inventory confirms the hold
        → CANCELLED         Cancel, only while PENDING
        → PAYMENT_FAILED    a payment.failed event
        → REFUND_REQUIRED   ConfirmOrder: inventory refused the confirm

CANCELLED | PAYMENT_FAILED | TIMEOUT → REFUND_REQUIRED   a payment settled after the order ended
REFUND_REQUIRED → REFUNDED                              nothing writes this yet
```

`TIMEOUT` is read but never written: an unpaid order stays `PENDING`, and its inventory hold
expires on its own (`internal/workflows/shared.go`, `docs/RESERVATION_HOLD.md`).

---

## Temporal Workflows

### 1. CreateOrder Workflow

**Location:** `internal/workflows/create_order.go`

**Workflow ID:** `CreateOrder:{orderCode}`

**Task Queue:** `create-order-tasks`

**Purpose:** Orchestrate order creation with saga pattern compensation

**Steps:**
1. **Reserve Inventory** → Lock tickets under a row lock (Inventory Service)
   - Compensation: Release tickets
2. **Create Order** → Persist order record (DynamoDB)
   - Compensation: Delete order
3. **Create Order Items** → Persist order items (DynamoDB)
   - Compensation: Delete order items
4. **Create Payment Intent** → Generate payment URL (Payment Service)

Availability is never pre-checked: `Reserve` decides it under the lock, so a buyer who loses the
race leaves no order record behind.

**Compensation Flow:**
On any step failure, executes compensations in reverse order:
```
Delete Items → Delete Order → Release Inventory
```

**Configuration:**
- Activity Timeout: 2 minutes
- Retry Policy: 5 attempts with exponential backoff
- Reservation hold: `PaymentTimeout` + `ReservationHoldGrace` = 9 minutes
  (`internal/workflows/shared.go`), so the hold strictly outlives the payment window

**Result:**
```go
CreateOrderWorkflowResult {
    PaymentUrl string
    Order      *models.Order
    OrderItems []models.OrderItem
}
```

---

### 2. ConfirmOrder Workflow

**Location:** `internal/workflows/confirm_order.go`

**Workflow ID:** `ConfirmOrder:{orderCode}`

**Task Queue:** `confirm-order-tasks`

**Purpose:** Finalize order after successful payment

**Steps:**
1. **Get Order** → Retrieve order from DB and validate status is PENDING
2. **Confirm Inventory** → Convert reservation to permanent sale (Inventory Service)
3. **Update Order Status** → Mark order as COMPLETED, publish `checkout.completed`

**Configuration:**
- Activity Timeout: 5 minutes
- Retry Policy: 10 attempts with exponential backoff

**Paid but unfulfillable:**
- Inventory refusing the confirm (the hold expired and the stock was resold), or a payment
  landing on an order already cancelled, failed or timed out, moves the order to
  `REFUND_REQUIRED` and publishes `order.refund_required` (`markForRefund`).
- Inventory being unavailable is not a refusal: the order stays `PENDING` for a redelivered
  payment event, because whether it can still be fulfilled is unknown.
- Nothing consumes that topic yet, so the state is a manual-reconciliation signal. The
  `OrdersNeedingRefund` alert fires on it — see `docs/RUNBOOK.md`.

---

## Activities

### Order Activities (`internal/activities/order.go`)
- **CreateOrder** - Persist order record
- **CreateOrderItems** - Persist order items
- **GetOrder** - Retrieve order by code
- **UpdateOrderStatus** - Update order status
- **DeleteOrder** - Delete order (compensation)
- **DeleteOrderItems** - Delete order items (compensation)

### Inventory Activities (`internal/activities/inventory.go`)
- **CheckAvailability** - Verify ticket availability
- **ReserveInventory** - Lock tickets temporarily (9-minute hold: payment window + grace)
- **ConfirmInventory** - Permanently commit reservation
- **ReleaseInventory** - Cancel reservation (compensation)

### Payment Activities (`internal/activities/payment.go`)
- **CreatePaymentIntent** - Generate payment URL with provider
- **CancelPayment** - Cancel payment (placeholder - TODO)

### Event Activities (`internal/activities/event.go`)
- **GetEvent** - Retrieve event details
- **GetEventConfig** - Retrieve event configuration (waitroom settings)

---

## Kafka Integration

### Topics

**Consumed:**
- `payment.completed` - Payment successfully processed
- `payment.failed` - Payment failed or declined

**Published:**
- `checkout.completed` - Order successfully completed
- `checkout.failed` - Order creation or payment failed

### Event Schemas

**PaymentCompletedEvent:**
```go
{
    OrderCode      string
    PaymentID      string
    AmountCents    int64
    Currency       string
    Provider       string
    TransactionID  string
    PaidAt         time.Time
}
```

**PaymentFailedEvent:**
```go
{
    OrderCode      string
    PaymentID      string
    AmountCents    int64
    Currency       string
    Provider       string
    TransactionID  string
    FailedAt       time.Time
}
```

**CheckoutCompletedEvent:**
```go
{
    SessionID  string
    UserID     string
    EventID    string
    Timestamp  time.Time
}
```

**CheckoutFailedEvent:**
```go
{
    SessionID  string
    UserID     string
    EventID    string
    Timestamp  time.Time
}
```

### Consumer Flow

**Payment Completed:**
1. Consumer receives event → `HandlePaymentCompleted()`
2. Starts `ConfirmOrder` Temporal workflow
3. Workflow confirms inventory and updates order status
4. Publishes `checkout.completed` event (frees waitroom slot)

**Payment Failed:**
1. Consumer receives event → `HandlePaymentFailed()`
2. Releases reserved tickets via Inventory Service
3. Updates order status to PAYMENT_FAILED
4. Publishes `checkout.failed` event

---

## Service Layer

### Key Components

**Service Interface:** `internal/order/interface.go`
```go
type Service interface {
    Create(ctx, CreateOrderInput) (CreateOrderOutput, error)
    Cancel(ctx, CancelOrderInput) (CancelOrderOutput, error)
    Get(ctx, GetOrderInput) (GetOrderOutput, error)
    GetMany(ctx, GetManyOrdersInput) (GetManyOrdersOutput, error)
    List(ctx, ListOrdersInput) (ListOrdersOutput, error)
}
```

**Implementation:** `internal/order/service/order.go`

**Dependencies:**
- Repository (DynamoDB data access)
- JWT Manager (checkout token validation)
- Temporal Client (workflow execution)
- Kafka Producer (event publishing)
- gRPC Clients (Event, Inventory, Payment services)

### Key Methods

**Create Order:**
1. Validate request parameters
2. Get event details and config from Event Service
3. Validate checkout token if waitroom enabled (JWT verification)
4. Fetch ticket class details from Inventory Service
5. Calculate total amount
6. Generate unique order code
7. Start CreateOrder Temporal workflow
8. Return order details and payment URL

**Cancel Order:**
1. Validate order exists and is PENDING
2. Release reserved tickets via Inventory Service
3. Update order status to CANCELLED
4. Publish `checkout.failed` event if session ID exists

**Token Validation:**
- JWT verification with HMAC signing
- Claims validation: UserID, EventID must match request
- Token expiry check

---

## gRPC API

### Endpoints

**CreateOrder** (`CreateOrderRequest → CreateOrderResponse`)
- Initiates order creation workflow
- Returns order details and payment URL

**CancelOrder** (`CancelOrderRequest → CancelOrderResponse`)
- Cancels pending order
- Releases reserved tickets

**GetOrder** (`GetOrderRequest → GetOrderResponse`)
- Retrieves order by ID or code

**GetManyOrders** (`GetManyOrdersRequest → GetManyOrdersResponse`)
- Paginated order list with filters

**ListOrders** (`ListOrdersRequest → ListOrdersResponse`)
- Filtered order list for admin

### Error Codes

Custom gRPC errors with codes (ORD001-ORD017):
- **ORD001** - Order not found
- **ORD002** - Order already exists
- **ORD003** - Invalid order status
- **ORD004** - Order creation failed
- **ORD005** - Order update failed
- **ORD006** - Order cancellation failed
- **ORD007** - Order not pending
- **ORD008** - Event not found
- **ORD009** - Event not ready for sale
- **ORD010** - Ticket class not found
- **ORD011** - Tickets sold out
- **ORD012** - Not enough tickets
- **ORD013** - Checkout expired
- **ORD014** - Invalid checkout token
- **ORD015** - Checkout token already used
- **ORD016** - Event config not found
- **ORD017** - Payment amount mismatch

---

## External Service Dependencies

### Event Service (localhost:50053)
**Operations:**
- `FindOne` - Get event details by ID
  - Validates event status is PUBLISHED
- `GetConfig` - Get event configuration
  - Returns waitroom settings (AllowWaitRoom)

### Inventory Service (localhost:50057)
**Operations:**
- `CheckAvailability` - Verify tickets available for event
- `FindManyTicketClass` - Get ticket class details and pricing
- `Reserve` - Lock tickets for the payment window plus grace (9 minutes)
  - Uses order code as reservation ID
  - Returns reservation expiration time
- `Confirm` - Permanently commit reservation
  - Decrements reserved, increments sold
- `Release` - Cancel reservation
  - Returns tickets to available pool

### Payment Service (localhost:50055)
**Operations:**
- `CreatePaymentIntent` - Generate payment URL
  - Supports providers: VNPAY, ZALOPAY, PAYOS
  - Idempotency key: `{orderCode}:{provider}`
  - Returns payment URL for user redirect

---

## Configuration

**All configs loaded from environment variables**

### Server
```env
SERVER_GRPC_PORT=50054
SERVER_READ_TIMEOUT=30s
SERVER_WRITE_TIMEOUT=30s
SERVER_IDLE_TIMEOUT=60s
PAYMENT_TIMEOUT_SECONDS=600  # loaded, never read: the window is PaymentTimeout in internal/workflows/shared.go
```

### Database (DynamoDB)
```env
DYNAMODB_TABLE_NAME=ticketbottle-orders
AWS_REGION=us-east-1
DYNAMODB_ENDPOINT=  # Empty for AWS; http://localhost:8000 for docker-compose.dev.yml
```

### JWT
```env
JWT_SECRET=your-super-secret-key-change-in-production
JWT_EXPIRY=15m
```

### Kafka
```env
KAFKA_BROKERS=localhost:9092
KAFKA_PRODUCER_RETRY_MAX=3
KAFKA_PRODUCER_REQUIRED_ACKS=1
KAFKA_ENABLED=true
KAFKA_CONSUMER_GROUP_ID=order-service
```

### Microservices
```env
EVENT_SERVICE_ADDR=localhost:50053
INVENTORY_SERVICE_ADDR=localhost:50057
PAYMENT_SERVICE_ADDR=localhost:50055
```

### Temporal
```env
TEMPORAL_HOST_PORT=localhost:7233
TEMPORAL_NAMESPACE=default
```

### Logging
```env
LOG_LEVEL=info
LOG_MODE=development
LOG_ENCODING=console
```

---

## Complete Order Flow

### 1. Order Creation Flow

```
User → API Gateway → Order Service gRPC

1. Validate Request
   - Check required fields
   - Validate ticket items

2. Get Event Details (Event Service)
   - Verify event exists
   - Check status is PUBLISHED
   - Get event configuration

3. Validate Checkout Token (if waitroom enabled)
   - Verify JWT signature
   - Check UserID, EventID match
   - Validate token not expired

4. Fetch Ticket Classes (Inventory Service)
   - Get pricing for each ticket class
   - Calculate total amount
   - Validate currency

5. Start CreateOrder Workflow (Temporal)
   - Generate unique order code
   - Execute saga steps:
     a. Reserve inventory (9-minute hold, decided under a row lock)
     b. Create order record
     c. Create order items
     d. Create payment intent

6. Return Response
   - Order details
   - Payment URL
```

### 2. Payment Completion Flow

```
Payment Provider → Payment Service → Kafka → Order Consumer

1. Kafka Event Received
   - Topic: payment.completed
   - Extract order code

2. Start ConfirmOrder Workflow (Temporal)
   - Get order from DB
   - Validate status is PENDING
   - Confirm inventory (permanent commit)
   - Update order status to COMPLETED
   - Set paid_at timestamp

3. Publish Checkout Event (Kafka)
   - Topic: checkout.completed
   - Notify waitroom to free slot
```

### 3. Payment Failure Flow

```
Payment Provider → Payment Service → Kafka → Order Consumer

1. Kafka Event Received
   - Topic: payment.failed
   - Extract order code

2. Handle Payment Failure
   - Get order from DB
   - Release reserved inventory
   - Update status to PAYMENT_FAILED

3. Publish Checkout Event (Kafka)
   - Topic: checkout.failed
   - Notify waitroom
```

### 4. Order Cancellation Flow

```
User → API Gateway → Order Service gRPC

1. Validate Order
   - Check order exists
   - Verify status is PENDING

2. Release Inventory
   - Call Inventory Service to release reservation

3. Update Order Status
   - Set status to CANCELLED

4. Publish Event (if session exists)
   - Topic: checkout.failed
   - Free waitroom slot
```

---

## Error Handling Strategy

### Domain Errors
All errors defined in `internal/order/errors.go` with clear semantics

### Workflow Error Handling
- Activities wrapped in compensation tracking
- Automatic retry with exponential backoff
- Compensations executed in reverse order on failure

### gRPC Error Mapping
- All domain errors mapped to gRPC status codes
- Custom error codes (ORD001-ORD017) for client handling
- Detailed error messages for debugging

### Critical Failure Handling
- Temporal workflow history preserved for debugging
- TODO: Implement alerting for critical failures
- TODO: Manual intervention system for stuck workflows

---

## Monitoring & Observability

### Logging
- Structured logging with Zap
- Context-aware logging throughout codebase
- Request/response logging via gRPC interceptor
- Format: ISO8601 timestamps, JSON or console encoding

### Temporal UI
- Workflow execution tracking
- Activity logs and retry history
- Compensation execution visibility
- Workflow history for debugging

### Key Metrics to Monitor
- Order creation success/failure rate
- Workflow execution time
- Activity retry counts
- Kafka consumer lag
- Payment timeout rate
- Inventory confirmation failures

---

## Security Considerations

### JWT Token Validation
- HMAC-SHA256 signing
- Token expiry enforcement
- Claims validation (UserID, EventID)
- Used for checkout token verification

### gRPC Communication
- Internal services on private network
- TODO: Add mTLS for production
- TODO: Add rate limiting

### Data Protection
- Soft delete for audit trail
- No PII in logs
- JWT secret rotation recommended

---

## Future Enhancements (TODOs)

1. **Alerting System**
   - Critical workflow failures
   - Payment timeout alerts
   - Inventory confirmation failures

2. **Manual Intervention**
   - Admin UI for stuck workflows
   - Manual compensation triggers
   - Order status override

3. **Payment Cancellation**
   - Implement cancel payment activity
   - Add to compensation flow

4. **Enhanced Events**
   - Add more metadata to checkout events
   - Event versioning
   - Schema registry

5. **Observability**
   - Distributed tracing (Jaeger/Zipkin)
   - Prometheus metrics
   - Custom dashboards

6. **Refund Support**
   - Implement refund workflow
   - Inventory return logic
   - Refund status tracking

---

## Common Issues & Troubleshooting

### Issue: Order stuck in PENDING status
**Cause:** Payment webhook not received or Kafka consumer down
**Resolution:**
- Check Kafka consumer health
- Verify payment service webhook configuration
- Manually trigger ConfirmOrder workflow via Temporal UI

### Issue: Nil pointer dereference in token validation
**Cause:** JWT manager not initialized
**Resolution:** Ensure JWT manager passed to service constructor in main.go

### Issue: Inventory reservation timeout
**Cause:** Payment took longer than the 6-minute payment window
**Resolution:**
- Check payment service performance
- Consider increasing reservation TTL
- Monitor payment completion latency

### Issue: Workflow compensation not executing
**Cause:** Activity panic or non-deterministic code
**Resolution:**
- Check activity implementation for panics
- Ensure compensation tracking is correct
- Review Temporal workflow history

---

## Development Notes

### Testing Strategy
- Unit tests for service layer
- Integration tests with Temporal test server
- Mock external services for testing
- Repository tests against DynamoDB Local

### Code Organization
- Clear separation of concerns (delivery, service, repository)
- Dependency injection throughout
- Interface-based design for testability

### Best Practices
- Context passed to all layers
- Structured error handling
- Idempotent operations where possible
- Compensation logic mirrors forward logic

---

## Related Documentation

- Temporal Workflows: https://docs.temporal.io/
- Kafka Sarama: https://github.com/IBM/sarama
- AWS SDK for Go v2 (DynamoDB): https://aws.github.io/aws-sdk-go-v2/docs/
- gRPC Go: https://grpc.io/docs/languages/go/

---

**Last Updated:** 2025-11-13
**Version:** 1.0
**Maintainer:** Order Service Team
