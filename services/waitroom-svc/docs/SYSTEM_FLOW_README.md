# TicketBottle Waitroom System Flow & Architecture

## System Overview

The waitroom service implements a **virtual queue system** for high-demand ticket sales using **Redis for queue management** and **Kafka for event streaming**. Clients discover their position, and their checkout token, by polling `GetQueueStatus`.

## System Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                       WAITROOM SERVICE                                │
├──────────────────────────────────────────────────────────────────────┤
│                                                                        │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐           │
│  │              │    │              │    │              │           │
│  │ gRPC Server  │    │   Kafka      │    │   Kafka      │           │
│  │  (Port 50056)│    │  Producer    │    │  Consumer    │           │
│  │              │    │              │    │              │           │
│  │ - JoinQueue  │    │ Publishes:   │    │ Consumes:    │           │
│  │ - GetStatus  │    │ - JOINED      │    │ - COMPLETED   │           │
│  │ - LeaveQueue │    │ - LEFT        │    │ - FAILED      │           │
│  │ - GetStatus  │    │ - READY       │    │ - EXPIRED     │           │
│  │              │    │              │    │              │           │
│  └───────┬──────┘    └──────┬───────┘    └──────┬───────┘           │
│          │                  │                   │                    │
│          └──────────────────┼───────────────────┘                    │
│                             │                                        │
│                    ┌────────▼────────┐                               │
│                    │                 │                               │
│                    │  Services Layer │                               │
│                    │  - Queue        │                               │
│                    │  - Session      │                               │
│                    │  - Waitroom     │                               │
│                    │  - Processor     │                               │
│                    │                 │                               │
│                    └────────┬────────┘                               │
│                             │                                        │
│                    ┌────────▼────────┐                               │
│                    │  Redis Storage  │                               │
│                    │  - Sessions     │                               │
│                    │  - Queues       │                               │
│                    │  - Processing   │                               │
│                    └─────────────────┘                               │
│                                                                        │
│     Queue Processor: Running in background (every 1s)                │
│     Position discovery: clients poll GetQueueStatus                  │
│                                                                        │
└──────────────────────────────────────────────────────────────────────┘
```

## Complete System Flow

### 1. User Joins Queue

```
User → gRPC → WaitroomService.JoinQueue()
  ├─ SessionService.CreateSession() → Redis
  ├─ QueueService.EnqueueSession() → Redis Sorted Set
  ├─ Kafka Producer: PublishQueueJoined() (EXTERNAL)
  └─ Return: position, session_id, queue_length
```

**Files:**
- [internal/service/waitroom_service.go](../internal/service/waitroom_service.go) - JoinQueue()
- [internal/service/queue_service.go:40-66](../internal/service/queue_service.go#L40-L66) - EnqueueSession()
- [internal/repository/redis/queue_repository.go](../internal/repository/redis/queue_repository.go) - Redis operations

### 2. Position Discovery

```
User → gRPC → GetQueueStatus(session_id)
  ├─ Read the session
  ├─ queued   → ZRANK for position, plus queue length
  └─ admitted → checkout token, URL and expiry
```

A push-based stream was removed: it held an SSE connection, a gRPC stream and a
Redis Pub/Sub subscription per waiter, and every admission cost three Redis
operations per waiter. See `../CLAUDE.md`, "Admission is discovered by polling".

**Files:**
- [internal/delivery/grpc/service.go](../internal/delivery/grpc/service.go) - GetQueueStatus()
- [internal/service/queue_service.go](../internal/service/queue_service.go) - GetQueueStatus()

### 3. Queue Processing

```
Background Goroutine (Every 1 second):
  ├─ Get active events from Event Service
  ├─ For each event:
  │   ├─ Check available checkout slots (max 100)
  │   ├─ Calculate: available = maxConcurrent - processingCount
  │   ├─ Pop N users from front of queue
  │   ├─ For each user:
  │   │   ├─ Generate JWT checkout token
  │   │   ├─ Update session status to "admitted"
  │   │   ├─ Add to processing set (15min TTL)
  │   │   └─ Kafka: PublishQueueReady() (EXTERNAL)
  │   └─ Release batch (default: 10 users per batch)
  └─ Repeat
```

**Files:**
- [internal/service/queue_processor.go](../internal/service/queue_processor.go) - Complete processor implementation
- [cmd/api/main.go:105-109](../cmd/api/main.go#L105-L109) - Processor startup
- [cmd/api/main.go:138-140](../cmd/api/main.go#L138-L140) - Graceful shutdown

**Key Methods:**
- `Start()` - Starts the background processor
- `ProcessEventQueue()` - Processes a single event's queue
- `admitUserToCheckout()` - Admits one user to checkout

### 4. User Gets Checkout Access

```
User polls GetQueueStatus():
  ├─ If status = "queued"   → Show position
  └─ If status = "admitted" → Show checkout token + URL
```

### 5. Checkout Process

```
Checkout Service receives QUEUE_READY event:
  ├─ Validates JWT checkout token
  ├─ Reserves tickets for user
  ├─ User completes payment
  └─ Publishes CHECKOUT_COMPLETED/FAILED/EXPIRED
```

### 6. Cleanup & Next User

```
Waitroom consumes checkout completion events:
  ├─ HandleCheckoutCompleted/Failed/Expired()
  ├─ Update session status
  ├─ Remove from processing set
  ├─ Free slot for next user
  └─ Processor automatically admits next user in queue
```

**Files:**
- [internal/delivery/kafka/consumer/consumer.go](../internal/delivery/kafka/consumer/consumer.go)
- [internal/service/waitroom_service.go](../internal/service/waitroom_service.go) - HandleCheckout methods

## Redis Data Structures

The service uses **four Redis data structures** per event:

### 1. Sessions (String with JSON)

```redis
# Key: session:{session_id}
# Value: JSON of session object
# TTL: 2 hours
GET session:abc-123

{
  "id": "abc-123",
  "user_id": "user-456",
  "event_id": "concert-2024",
  "status": "queued",        # queued → admitted → completed
  "position": 42,
  "checkout_token": "",      # Generated when admitted
  "queued_at": "2024-10-13T10:30:00Z",
  "expires_at": "2024-10-13T12:30:00Z"
}
```

### 2. Queue (Sorted Set)

```redis
# Key: waitroom:{event_id}:queue
# Score: timestamp (FIFO)
# Members: session_ids

ZRANGE waitroom:concert-2024:queue 0 -1 WITHSCORES
1) "session-abc-123"  # First in line
2) "1696248000"       # Joined at timestamp
3) "session-def-456"  # Second in line
4) "1696248015"       # Joined 15 seconds later
```

### 3. Processing Set (Sorted Set)

```redis
# Key: waitroom:{event_id}:checkouts
# Members: session_ids of users currently in checkout
# Score: absolute slot expiry (unix ms) -- each slot expires on its own
#        schedule and is reaped on the next count

ZRANGE waitroom:concert-2024:checkouts 0 -1
1) "session-xyz-789"  # User in checkout
2) "session-uvw-012"  # User in checkout
# Max 100 concurrent users (configurable)
```

### 4. Buffered `queue.ready` Retries

```redis
# List: waitroom:queue_ready:pending
# Holds payloads whose Kafka publish failed; drained at the head of the next tick.

LRANGE waitroom:queue_ready:pending 0 -1

# Receives real-time updates when:
# - User joins queue (user_joined)
# - User leaves queue (user_left)
# - User admitted to checkout (user_admitted)
```

## Kafka Event Flow

### Events published (producer)

| Event | Topic | When | Purpose |
|-------|-------|------|---------|
| **QUEUE_JOINED** | `queue.joined` | User joins queue | Analytics, notifications, monitoring |
| **QUEUE_LEFT** | `queue.left` | User leaves queue | Track abandonment rate |
| **QUEUE_READY** | `queue.ready` | User admitted to checkout | Notify Checkout Service |

**File:** [internal/delivery/kafka/producer/producer.go](../internal/delivery/kafka/producer/producer.go)

### Events consumed (consumer)

| Event | Topic | When | Handler |
|-------|-------|------|---------|
| **CHECKOUT_COMPLETED** | `checkout.completed` | Payment success | Free slot, update session |
| **CHECKOUT_FAILED** | `checkout.failed` | Payment failed | Free slot, mark failed |
| **CHECKOUT_EXPIRED** | `checkout.expired` | 15-min timeout | Free slot, mark expired |

**File:** [internal/delivery/kafka/consumer/consumer.go](../internal/delivery/kafka/consumer/consumer.go)

## Kafka

Redis Pub/Sub was used for client streaming and is gone with it. Redis now holds
queue state only; Kafka carries everything that crosses a service boundary.

### Kafka Events (External Service-to-Service)
- **Scope:** External (between microservices)
- **Purpose:** Service-to-service communication
- **Consumers:** Analytics, Notification, Admin services
- **Latency:** ~5-50ms
- **Durability:** Persistent (stored, replayable)
- **Use case:** Notify other services about queue events

**Both are necessary** - Redis for instant client updates, Kafka for reliable service communication.

## Configuration

Key config values that control queue behavior:

```bash
# Queue Processing
QUEUE_DEFAULT_MAX_CONCURRENT=100   # Max users in checkout per event
QUEUE_DEFAULT_RELEASE_RATE=10      # Users admitted per batch
QUEUE_PROCESS_INTERVAL=1s          # How often processor runs
QUEUE_SESSION_TTL=7200s            # Session expiry (2 hours)

# Redis
REDIS_ADDR=localhost:6379
REDIS_POOL_SIZE=10

# Kafka
KAFKA_ENABLED=true
KAFKA_BROKERS=localhost:9092
KAFKA_CONSUMER_GROUP_ID=waitroom-service

# gRPC Server
SERVER_GRPC_PORT=50056
```

**File:** [config/config.go](../config/config.go)

## Testing the System

### Prerequisites

```bash
# 1. Start infrastructure
docker-compose up -d redis zookeeper kafka

# 2. Start waitroom service
go run cmd/api/main.go

# 3. Verify services
docker ps
# Should show: redis, zookeeper, kafka all running
```

### Test 1: Join Queue and Check Status

```bash
# Join queue
grpcurl -plaintext -d '{
  "user_id": "user1",
  "event_id": "concert-2024"
}' localhost:50056 waitroom.v1.WaitroomService/JoinQueue

# Response:
{
  "session_id": "session-abc-123",
  "position": 1,
  "queue_length": 1,
  "queued_at": "2024-01-15T10:30:00Z",
  "expires_at": "2024-01-15T12:30:00Z"
}

# Check status (poll)
grpcurl -plaintext -d '{
  "session_id": "session-abc-123"
}' localhost:50056 waitroom.v1.WaitroomService/GetQueueStatus

# Initially: status = "queued", position = 1
# After processor runs: status = "admitted", checkout_token populated
```

### Test 2: Position Discovery

```bash
grpcurl -plaintext -d '{
  "session_id": "session-abc-123"
}' localhost:50056 waitroom.v1.WaitroomService/GetQueueStatus

# queued   -> position and queue_length
# admitted -> checkout token, URL and expiry
```

### Test 3: Multiple Users

```bash
# User 1 joins
grpcurl -plaintext -d '{"user_id":"user1","event_id":"concert1"}' \
  localhost:50056 waitroom.v1.WaitroomService/JoinQueue

# User 2 joins
grpcurl -plaintext -d '{"user_id":"user2","event_id":"concert1"}' \
  localhost:50056 waitroom.v1.WaitroomService/JoinQueue

# User 1 polls; queue_length should read 2
grpcurl -plaintext -d '{"session_id":"session-1"}' \
  localhost:50056 waitroom.v1.WaitroomService/GetQueueStatus
```

### Test 4: Verify Redis Data

```bash
# Check queue
redis-cli ZRANGE waitroom:concert-2024:queue 0 -1 WITHSCORES

# Check processing set
redis-cli ZRANGE waitroom:concert-2024:checkouts 0 -1

# Check session
redis-cli GET session:abc-123
```

### Test 5: Verify Kafka Events

```bash
# Monitor Kafka topics
kafka-console-consumer --bootstrap-server localhost:9092 \
  --topic queue.joined --from-beginning

kafka-console-consumer --bootstrap-server localhost:9092 \
  --topic queue.ready --from-beginning
```

## System Metrics

The queue processor tracks:
- **IsRunning:** Processor status
- **StartedAt:** When processor started
- **LastProcessed:** Last processing timestamp
- **EventsActive:** Number of events being processed
- **TotalAdmitted:** Total users admitted to checkout
- **ErrorCount:** Failed operations count

Access via: `wrSvc.GetProcessorStatus()`

## Monitoring & Debugging

### Check Processor Status

```bash
# View logs
docker logs waitroom-service | grep -i "queue processor"

# Should see:
# - "Queue processor started"
# - "Processing event queue" (every 1s)
# - "Admitted user to checkout"
```

### Check Redis Health

```bash
redis-cli PING  # Should return PONG
redis-cli INFO stats
redis-cli LLEN waitroom:queue_ready:pending  # 0 unless Kafka is failing
```

### Check Kafka Health

```bash
kafka-topics --bootstrap-server localhost:9092 --list
# Should show: queue.joined, queue.left, queue.ready
```

## Key Files Reference

### Core Services
- [internal/service/waitroom_service.go](../internal/service/waitroom_service.go) - Main service orchestration
- [internal/service/queue_service.go](../internal/service/queue_service.go) - Queue operations
- [internal/service/session_service.go](../internal/service/session_service.go) - Session management
- [internal/service/queue_processor.go](../internal/service/queue_processor.go) - Background processor

### Delivery Layer
- [internal/delivery/grpc/service.go](../internal/delivery/grpc/service.go) - gRPC handlers
- [internal/delivery/kafka/producer/producer.go](../internal/delivery/kafka/producer/producer.go) - Kafka producer
- [internal/delivery/kafka/consumer/consumer.go](../internal/delivery/kafka/consumer/consumer.go) - Kafka consumer

### Repository Layer
- [internal/repository/redis/queue_repository.go](../internal/repository/redis/queue_repository.go) - Redis queue ops
- [internal/repository/redis/session_repository.go](../internal/repository/redis/session_repository.go) - Redis session ops

### Models
- [internal/models/session.go](../internal/models/session.go) - Session model

### Main Entry Point
- [cmd/api/main.go](../cmd/api/main.go) - Server initialization

## What the service covers

1. Queue management (join, leave, position tracking)
2. Background queue processor (automatic admission)
3. Position discovery by polling `GetQueueStatus`
4. Kafka event streaming (service-to-service)
5. Checkout token generation and validation
6. Graceful shutdown and error handling
7. Configuration through env vars (chart ConfigMaps in a cluster)
8. Processor metrics

Known limits are in `../CLAUDE.md`: the admission loop is single-replica only, and nothing
consumes the `.dlq` topics yet.

---


For queue processor details, see [QUEUE_PROCESSOR_GUIDE.md](QUEUE_PROCESSOR_GUIDE.md)
