import { JoinQueueResponse } from '@/protogen/waitroom.pb';
import { JoinQueueRespDto } from '../dtos/resp';

export class JoinQueueMapper {
  static toDto(proto: JoinQueueResponse): JoinQueueRespDto {
    return {
      sessionId: proto.sessionId,
      position: Number(proto.position),
      queueLength: Number(proto.queueLength),
      queuedAt: new Date(proto.queuedAt),
      expiresAt: new Date(proto.expiresAt),
      websocketUrl: proto.websocketUrl,
    };
  }
}
