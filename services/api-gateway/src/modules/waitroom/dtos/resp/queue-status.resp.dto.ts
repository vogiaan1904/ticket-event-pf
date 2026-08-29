import { SessionStatus } from '../../enums';

export class QueueStatusRespDto {
  sessionId: string;
  status: SessionStatus;
  position: number;
  queueLength: number;
  checkoutToken: string;
  checkoutUrl: string;
  checkoutExpiresAt: string;
}
