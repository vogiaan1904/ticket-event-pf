# Outbox Pattern Implementation Guide

## What is the Outbox Pattern?

The Outbox Pattern solves the **dual-write problem** in distributed systems where you need to:

1. Update the database
2. Publish an event to a message broker (Kafka, RabbitMQ, etc.)

### The Problem

```typescript
// ❌ NOT ATOMIC - Can fail between operations
await db.updatePayment(payment);
await kafka.publish(event); // What if this fails? Event is lost!
```

### The Solution

```typescript
// ✅ ATOMIC - Single database transaction
await db.transaction(async (tx) => {
  await tx.updatePayment(payment);
  await tx.saveToOutbox(event); // Same transaction!
});
// Background worker publishes from outbox
```

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  1. Business Logic (Payment Service)                │
│     - Update Payment Status                         │
│     - Save Event to Outbox (SAME TRANSACTION)       │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  2. Database (PostgreSQL)                           │
│     ┌──────────────┐    ┌─────────────────┐        │
│     │   Payment    │    │     Outbox      │        │
│     │   (updated)  │    │  (event saved)  │        │
│     └──────────────┘    └─────────────────┘        │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  3. Background Worker (OutboxPublisherService)      │
│     - Polls outbox every 5 seconds                  │
│     - Publishes events to Kafka                     │
│     - Marks as published                            │
│     - Retries on failure (max 5 times)              │
└─────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────┐
│  4. Kafka / Message Broker                          │
│     - Receives events reliably                      │
│     - Other services consume events                 │
└─────────────────────────────────────────────────────┘
```

## Implementation Steps

### 1. Database Schema (Already Added)

```prisma
model Outbox {
  id            String        @id @default(uuid())
  aggregateId   String        // Payment ID
  aggregateType String        // "Payment"
  eventType     String        // "payment.completed"
  payload       Json          // Event data
  published     Boolean       @default(false)
  publishedAt   DateTime?
  createdAt     DateTime      @default(now())
  retryCount    Int           @default(0)
  lastError     String?

  @@index([published, createdAt])
  @@map("outbox")
}
```

### 2. Run Migration

```bash
# Generate Prisma client with new Outbox model
npx prisma generate

# Create and run migration
npx prisma migrate dev --name add_outbox_pattern
```

### 3. Install Required Dependencies

```bash
# For cron jobs (background worker)
npm install @nestjs/schedule
```

### 4. Update AppModule

```typescript
import { Module } from '@nestjs/common';
import { ScheduleModule } from '@nestjs/schedule';

@Module({
  imports: [
    ScheduleModule.forRoot(), // Enable cron jobs
    // ... other imports
  ],
})
export class AppModule {}
```

### 5. Update PaymentModule

Add the new services to your PaymentModule providers:

```typescript
import { OutboxRepository } from './repository/outbox.repository';
import { PaymentOutboxService } from './payment.outbox';
import { OutboxPublisherService } from './services/outbox-publisher.service';

@Module({
  providers: [
    PaymentService,
    OutboxRepository, // NEW
    PaymentOutboxService, // NEW
    OutboxPublisherService, // NEW
    // ... other providers
  ],
})
export class PaymentModule {}
```

### 6. Update Repository Methods

Your `PaymentRepository` methods need to accept an optional transaction parameter:

```typescript
// Update these methods to support transactions
async create(dto: CreatePaymentIntentDto, tx?: Prisma.TransactionClient) {
  const prismaClient = tx || this.prisma;
  return prismaClient.payment.create({ /* ... */ });
}

async updateStatus(orderCode: string, status: PaymentStatus, tx?: Prisma.TransactionClient) {
  const prismaClient = tx || this.prisma;
  return prismaClient.payment.update({ /* ... */ });
}
```

## How It Works

### When Payment is Created

```typescript
await this.prisma.$transaction(async (tx) => {
  // 1. Create payment
  const payment = await this.repo.create(dto, tx);

  // 2. Save event to outbox (same transaction)
  await this.outboxService.savePaymentCreatedEvent(payment, dto.redirectUrl, tx);
});
// ✅ Both operations commit together or rollback together
```

### When Payment Completes

```typescript
await this.prisma.$transaction(async (tx) => {
  // 1. Update payment status
  const payment = await this.repo.updateStatus(orderCode, 'COMPLETED', tx);

  // 2. Save event to outbox (same transaction)
  await this.outboxService.savePaymentCompletedEvent(payment, tx);
});
// ✅ Atomicity guaranteed!
```

### Background Worker Publishes

```typescript
// Runs every 5 seconds
@Cron(CronExpression.EVERY_5_SECONDS)
async processOutboxMessages() {
  const messages = await this.outboxRepo.getUnpublished(50);

  for (const message of messages) {
    try {
      // Publish to Kafka
      await this.kafkaProducer.send({
        topic: 'payment-events',
        messages: [{ value: JSON.stringify(message.payload) }],
      });

      // Mark as published
      await this.outboxRepo.markAsPublished(message.id);
    } catch (error) {
      // Update retry count
      await this.outboxRepo.updateRetry(message.id, error.message);
    }
  }
}
```

## Benefits

1. **Atomicity**: DB update + event save happen together
2. **Reliability**: Events are never lost
3. **Retry Logic**: Failed publishes are automatically retried
4. **Idempotency**: Each event has unique ID
5. **Monitoring**: Track unpublished events and failures
6. **Cleanup**: Old published events are automatically cleaned up

## Event Types

```typescript
export enum PaymentEventType {
  PAYMENT_COMPLETED = 'payment.completed',
  PAYMENT_FAILED = 'payment.failed',
  PAYMENT_CREATED = 'payment.created',
}
```

## Event Payloads

```typescript
interface PaymentCompletedEvent {
  orderCode: string;
  paymentId: string;
  amountCents: number;
  currency: string;
  provider: string;
  transactionId: string;
  completedAt: Date;
}
```

## Monitoring

Query unpublished events:

```sql
SELECT * FROM outbox
WHERE published = false
ORDER BY created_at;
```

Query failed retries:

```sql
SELECT * FROM outbox
WHERE published = false
AND retry_count > 0
ORDER BY retry_count DESC;
```

## Next Steps

1. Run migrations to create Outbox table
2. Install @nestjs/schedule package
3. Update PaymentModule with new providers
4. Update PaymentRepository to support transactions
5. Integrate with Kafka when ready (currently logs events)
6. Monitor outbox table for stuck messages

## Kafka Integration (TODO)

When ready to integrate Kafka:

```typescript
// In OutboxPublisherService
await this.kafkaProducer.send({
  topic: 'payment-events',
  messages: [
    {
      key: message.aggregateId,
      value: JSON.stringify(message.payload),
      headers: {
        'event-type': message.eventType,
        'aggregate-type': message.aggregateType,
      },
    },
  ],
});
```
