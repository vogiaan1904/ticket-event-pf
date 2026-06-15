export class JoinQueueRespDto {
  sessionId: string;
  position: number;
  queueLength: number;
  queuedAt: Date;
  expiresAt: Date;
  websocketUrl: string;
}
