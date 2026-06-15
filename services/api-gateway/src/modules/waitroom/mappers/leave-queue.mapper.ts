import { LeaveQueueResponse } from '@/protogen/waitroom.pb';
import { LeaveQueueRespDto } from '../dtos/resp';

export class LeaveQueueMapper {
  static toDto(proto: LeaveQueueResponse): LeaveQueueRespDto {
    return {
      sessionId: proto.sessionId,
      message: proto.message,
    };
  }
}
