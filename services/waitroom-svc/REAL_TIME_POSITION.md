Professional Implementation Plan
Recommended: Option 1 (WebSocket + gRPC Stream)
Here's the step-by-step plan:
Phase 1: Backend Implementation (Waitroom Service)
Implement gRPC Server Streaming RPC
Add Redis Pub/Sub for Broadcasting Updates
When queue changes (join/leave/admit), publish event
Stream subscribers listen for their session updates
Use pattern: queue:position:{sessionID} or queue:event:{eventID}
Stream Implementation Strategy
Validate session on stream open
Subscribe to Redis channel for position updates
Send initial position immediately
Push updates when queue changes
Handle graceful disconnect
Set timeouts (e.g., close after session expires)
Trigger Points for Updates
User joins queue → broadcast to all sessions in that event
User leaves queue → broadcast position updates
User admitted to checkout → notify that session
Periodic position recalculation (every 5-10s)