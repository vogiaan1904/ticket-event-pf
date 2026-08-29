import { QueueStatusResponse } from '@/protogen/waitroom.pb';
import { QueueStatusRespDto } from '../dtos/resp';
import { SessionStatusMapper } from './session-status.mapper';

export class QueueStatusMapper {
  static toDto(proto: QueueStatusResponse): QueueStatusRespDto {
    return {
      sessionId: proto.sessionId,
      status: SessionStatusMapper.toEnum(proto.status),
      position: Number(proto.position),
      queueLength: Number(proto.queueLength),
      checkoutToken: proto.checkoutToken,
      checkoutUrl: proto.checkoutUrl,
      checkoutExpiresAt: proto.checkoutExpiresAt,
    };
  }
}
