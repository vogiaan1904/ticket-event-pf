# Real-Time Position Streaming Implementation Guide

## Overview

This guide documents the implementation of real-time queue position streaming using **gRPC Server Streaming + Redis Pub/Sub**.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                     REAL-TIME STREAMING FLOW                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Client                    Waitroom Service              Redis      │
│    │                              │                        │        │
│    │  1. JoinQueue                │                        │        │
│    ├─────────────────────────────>│                        │        │
│    │  <returns session_id>        │                        │        │
│    │                              │  2. ZADD to queue      │        │
│    │                              ├───────────────────────>│        │
│    │                              │  3. PUBLISH update     │        │
│    │                              ├───────────────────────>│        │
│    │                              │                        │        │
│    │  4. StreamQueuePosition      │                        │        │
│    ├─────────────────────────────>│                        │        │
│    │                              │  5. SUBSCRIBE channel  │        │
│    │                              ├───────────────────────>│        │
│    │  <initial position>          │                        │        │
│    │<─────────────────────────────┤                        │        │
│    │                              │                        │        │
│    │  [User 2 joins queue]        │                        │        │
│    │                              │  6. PUBLISH update     │        │
│    │                              ├───────────────────────>│        │
│    │  <position updated>          │  <receives message>    │        │
│    │<─────────────────────────────┤<───────────────────────┤        │
│    │                              │                        │        │
│    │  [Queue processor admits]    │                        │        │
│    │                              │  7. PUBLISH admitted   │        │
│    │                              ├───────────────────────>│        │
│    │  <admitted + token>          │  <receives message>    │        │
│    │<─────────────────────────────┤<───────────────────────┤        │
│    │                              │                        │        │
│    │  Stream closes               │  Unsubscribe           │        │
│    │                              ├───────────────────────>│        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

```
┌─────────────────────────────────────────────────────────────────────┐
│                    USER JOINS QUEUE FLOW                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Client Request                                                     │
│       │                                                             │
│       ↓                                                             │
│  ┌─────────────────────────────────────────┐                        │
│  │ Waitroom Service                        │                        │
│  │  1. Add to Redis Queue (ZADD)           │                        │
│  │  2. Get position                        │                        │
│  └─────┬───────────────────────┬───────────┘                        │
│        │                       │                                    │
│        │ Redis Pub/Sub         │ Kafka Event                        │
│        │ (INTERNAL)            │ (EXTERNAL)                         │
│        ↓                       ↓                                    │
│  ┌─────────────────┐     ┌──────────────────────────┐               │
│  │ Redis Channel   │     │ Kafka Topic              │               │
│  │ queue:updates:* │     │ "queue.joined"           │               │
│  └─────┬───────────┘     └──────┬─────────┬─────────┘               │
│        │                        │         │                         │
│        ↓                        │         │                         │
│  ┌─────────────────┐            │         │                         │
│  │ Active gRPC     │            ↓         ↓                         │
│  │ Streams         │     ┌─────────┐ ┌────────────┐                 │
│  │                 │     │Analytics│ │Notification│                 │
│  │ Client 1 ←──────┼─────┤ Service │ │  Service   │                 │
│  │ Client 2 ←──────┤     └─────────┘ └────────────┘                 │
│  │ Client 3 ←──────┤                                                │
│  └─────────────────┘          ↓             ↓                       │
│    "Position update"    "Track user"   "Send SMS"                   │
│    (instant)           (durable)       (durable)                    │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Components Implemented

### 1. Position Update Event Model
**File:** `internal/models/position_update.go`

```go
type PositionUpdateEvent struct {
    EventID            string
    UpdateType         UpdateType  // user_joined, user_left, user_admitted
    AffectedSessionIDs []string
    Timestamp          time.Time
}
```

### 2. Redis Pub/Sub Repository Methods
**File:** `internal/repository/redis/queue_repository.go`

- `PublishPositionUpdate(ctx, update)` - Publishes to `queue:updates:{eventID}`
- `SubscribeToPositionUpdates(ctx, eventID)` - Subscribes to position updates channel

**Redis Channels:**
- Pattern: `queue:updates:{eventID}`
- Example: `queue:updates:concert-2024`

### 3. Update Triggers

#### a) User Joins Queue
**File:** `internal/service/queue_service.go:EnqueueSession()`
- Triggers: `UpdateTypeUserJoined`
- Broadcasts to all sessions in the event's queue

#### b) User Leaves Queue
**File:** `internal/service/queue_service.go:DequeueSession()`
- Triggers: `UpdateTypeUserLeft`
- Broadcasts to all remaining sessions

#### c) User Admitted to Checkout
**File:** `internal/service/queue_processor.go:admitUserToCheckout()`
- Triggers: `UpdateTypeUserAdmitted`
- Broadcasts after successful admission

### 4. Service Layer Streaming
**File:** `internal/service/waitroom_service.go`

```go
StreamSessionPosition(ctx, sessionID, updates chan<- *PositionStreamUpdate) error
```

**Features:**
- Validates session on stream open
- Sends initial position immediately
- Subscribes to Redis Pub/Sub for the event
- Streams position changes in real-time
- Auto-closes when session status changes (admitted/completed/expired)
- Graceful disconnect on context cancellation

### 5. gRPC Streaming Handler
**File:** `internal/delivery/grpc/waitroom_service.go`

```go
StreamQueuePosition(req, stream) error
```

**Implements:**
- Protobuf `StreamQueuePosition` RPC
- Converts service updates to protobuf `PositionUpdate` messages
- Handles stream errors and client disconnections

## Testing Guide

### Prerequisites

```bash
# 1. Start infrastructure
make docker-up

# 2. Start waitroom service
make run

# 3. Verify services are running
docker ps
# Should show: redis, zookeeper, kafka
```

### Test 1: Basic Position Streaming

**Terminal 1: Start stream for User 1**
```bash
grpcurl -plaintext -d '{
  "user_id": "user1",
  "event_id": "concert-2024"
}' localhost:50056 waitroom.v1.WaitroomService/JoinQueue

# Note the session_id returned (e.g., "session-abc-123")

# Start streaming
grpcurl -plaintext -d '{
  "session_id": "session-abc-123"
}' localhost:50056 waitroom.v1.WaitroomService/StreamQueuePosition
```

**Expected Output:**
```json
{
  "session_id": "session-abc-123",
  "position": 1,
  "queue_length": 1,
  "status": "SESSION_STATUS_QUEUED",
  "updated_at": "2024-01-15T10:30:00Z"
}
```

**Terminal 2: Join more users to see position updates**
```bash
# User 2 joins
grpcurl -plaintext -d '{
  "user_id": "user2",
  "event_id": "concert-2024"
}' localhost:50056 waitroom.v1.WaitroomService/JoinQueue

# User 3 joins
grpcurl -plaintext -d '{
  "user_id": "user3",
  "event_id": "concert-2024"
}' localhost:50056 waitroom.v1.WaitroomService/JoinQueue
```

**Terminal 1 should receive updates:**
```json
{
  "session_id": "session-abc-123",
  "position": 1,
  "queue_length": 2,
  "status": "SESSION_STATUS_QUEUED",
  "updated_at": "2024-01-15T10:30:05Z"
}
{
  "session_id": "session-abc-123",
  "position": 1,
  "queue_length": 3,
  "status": "SESSION_STATUS_QUEUED",
  "updated_at": "2024-01-15T10:30:10Z"
}
```

### Test 2: Admission Flow

**Continue from Test 1:**

The queue processor runs every 1 second and will automatically admit users when slots are available.

**Terminal 1 should receive:**
```json
{
  "session_id": "session-abc-123",
  "position": 0,
  "queue_length": 2,
  "status": "SESSION_STATUS_READY",
  "updated_at": "2024-01-15T10:30:11Z",
  "checkout_token": "eyJhbGc...",
  "checkout_url": "/checkout"
}
```

Then the stream will close.

### Test 3: Multiple Concurrent Streams

**Start 3 streams in parallel:**

```bash
# Terminal 1 - User 1 stream
grpcurl -plaintext -d '{"session_id":"session-1"}' \
  localhost:50056 waitroom.v1.WaitroomService/StreamQueuePosition

# Terminal 2 - User 2 stream
grpcurl -plaintext -d '{"session_id":"session-2"}' \
  localhost:50056 waitroom.v1.WaitroomService/StreamQueuePosition

# Terminal 3 - User 3 stream
grpcurl -plaintext -d '{"session_id":"session-3"}' \
  localhost:50056 waitroom.v1.WaitroomService/StreamQueuePosition
```

All streams should receive updates when ANY user joins/leaves the queue.

### Test 4: Redis Pub/Sub Verification

```bash
# Monitor Redis pub/sub in real-time
redis-cli PSUBSCRIBE 'queue:updates:*'

# In another terminal, join queue
grpcurl -plaintext -d '{
  "user_id": "user-test",
  "event_id": "concert-2024"
}' localhost:50056 waitroom.v1.WaitroomService/JoinQueue

# You should see Redis pub/sub message:
# 1) "pmessage"
# 2) "queue:updates:*"
# 3) "queue:updates:concert-2024"
# 4) "{\"event_id\":\"concert-2024\",\"update_type\":\"user_joined\",...}"
```

### Test 5: Leave Queue Updates

```bash
# Terminal 1: Stream for user1
grpcurl -plaintext -d '{"session_id":"session-user1"}' \
  localhost:50056 waitroom.v1.WaitroomService/StreamQueuePosition

# Terminal 2: User2 leaves queue
grpcurl -plaintext -d '{"session_id":"session-user2"}' \
  localhost:50056 waitroom.v1.WaitroomService/LeaveQueue
```

**Terminal 1 should receive position update** showing queue_length decreased.

## Monitoring & Debugging

### Check Active Subscriptions
```bash
# Redis pub/sub channels info
redis-cli PUBSUB CHANNELS 'queue:updates:*'

# Number of subscribers per channel
redis-cli PUBSUB NUMSUB queue:updates:concert-2024
```

### Check Queue State
```bash
# Queue members
redis-cli ZRANGE waitroom:concert-2024:queue 0 -1 WITHSCORES

# Processing count
redis-cli ZCARD waitroom:concert-2024:checkouts

# Session details
redis-cli GET session:abc-123
```

### View Logs
```bash
# Filter streaming logs
docker logs waitroom-service 2>&1 | grep -i "stream\|position"

# Watch in real-time
docker logs -f waitroom-service | grep "StreamQueuePosition\|position update"
```

### Common Log Messages

**Stream Started:**
```
INFO  Started streaming position updates session_id=abc-123 event_id=concert-2024
```

**Position Update Published:**
```
DEBUG Published position update event_id=concert-2024 update_type=user_joined
```

**Position Update Sent:**
```
DEBUG Sent position update to client session_id=abc-123 position=1 status=queued
```

**Stream Closed:**
```
INFO  Session status changed, closing stream session_id=abc-123 status=admitted
```

## Configuration

### Environment Variables

```bash
# How often to check and process queues
QUEUE_PROCESS_INTERVAL=1s

# Position update interval (for periodic recalculation if needed)
QUEUE_POSITION_UPDATE_INTERVAL=5s

# Session TTL
QUEUE_SESSION_TTL=7200s

# Max users in checkout per event
QUEUE_DEFAULT_MAX_CONCURRENT=100

# Redis configuration (must be running)
REDIS_ADDR=localhost:6379
```

## Performance Considerations

### Scalability
- **Redis Pub/Sub** is lightweight and can handle thousands of concurrent subscribers
- Each event has its own channel (`queue:updates:{eventID}`)
- Position updates are broadcast only when queue changes (event-driven, not polling)

### Resource Usage
- **Memory:** Each stream maintains a small channel buffer (10 events)
- **CPU:** Minimal - only processes updates when queue changes
- **Network:** Efficient - only sends updates when position actually changes

### Recommended Limits
- **Max concurrent streams per event:** 10,000+
- **Position update frequency:** Event-driven (instant) + optional periodic (5s)
- **Stream timeout:** Tied to session TTL (2 hours)

## Error Handling

### Common Errors

#### 1. Invalid Session ID
```json
{
  "code": "InvalidArgument",
  "message": "session not found"
}
```

#### 2. Session Expired
```json
{
  "code": "NotFound",
  "message": "session expired"
}
```

#### 3. Redis Connection Lost
- Stream continues with best effort
- Logs warning: `Failed to publish position update`
- Does not fail user requests

## Integration with Other Services

### Checkout Service Integration
1. User receives `checkout_token` via stream when admitted
2. User calls Checkout Service with token
3. Checkout Service validates JWT token
4. On completion, Checkout publishes `CHECKOUT_COMPLETED` event
5. Waitroom removes from processing → frees slot for next user

### Event Service Integration
- Queue processor queries active events
- Only processes queues for active events
- Inactive events won't trigger position updates

## Next Steps

### Optional Enhancements
1. **Heartbeat Messages:** Send periodic keep-alive to detect disconnected clients
2. **Position Prediction:** Estimate wait time based on queue length and processing rate
3. **Priority Queues:** VIP users get priority positions
4. **Analytics:** Track average wait time, admission rate, etc.

## Troubleshooting

### Issue: No Position Updates Received

**Check:**
```bash
# 1. Verify Redis is running
redis-cli PING

# 2. Check if updates are being published
redis-cli MONITOR | grep "PUBLISH queue:updates"

# 3. Verify queue processor is running
docker logs waitroom-service | grep "queue processor"

# 4. Check session is in queue
redis-cli ZRANGE waitroom:concert-2024:queue 0 -1
```

### Issue: Stream Closes Immediately

**Possible causes:**
- Session already admitted/completed/expired
- Invalid session ID
- Session not in queue

**Debug:**
```bash
# Check session status
grpcurl -plaintext -d '{"session_id":"xxx"}' \
  localhost:50056 waitroom.v1.WaitroomService/GetQueueStatus
```

### Issue: High Redis CPU Usage

**Possible causes:**
- Too many position updates being published
- Consider batching updates or adding rate limiting

**Monitor:**
```bash
redis-cli INFO stats | grep pubsub
```

## Summary

- Real-time position streaming, end to end:

**Key Features:**
- gRPC Server Streaming for real-time updates
- Redis Pub/Sub for efficient broadcasting
- Event-driven updates (instant notification)
- Graceful handling of admission, expiry, and disconnection
- Scalable to thousands of concurrent streams

**Files Modified:**
- `internal/models/position_update.go` (NEW)
- `internal/repository/redis/queue_repository.go` (+pub/sub methods)
- `internal/service/queue_service.go` (+broadcast triggers)
- `internal/service/queue_processor.go` (+broadcast trigger)
- `internal/service/waitroom_service.go` (+StreamSessionPosition)
- `internal/service/types.go` (+PositionStreamUpdate)
- `internal/delivery/grpc/waitroom_service.go` (+StreamQueuePosition)
- `.env` (documented config)

**Ready for production!** 🚀
