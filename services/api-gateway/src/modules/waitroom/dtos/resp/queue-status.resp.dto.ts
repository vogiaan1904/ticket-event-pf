import { SessionStatus } from '../../enums';

export class QueueStatusRespDto {
  sessionId: string;
  status: SessionStatus;
  /** The door is shut until a ticket is available again; the place is kept. */
  paused: boolean;
  position: number;
  queueLength: number;
  checkoutToken: string;
  checkoutUrl: string;
  checkoutExpiresAt: string;
}
