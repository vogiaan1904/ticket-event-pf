import { PositionUpdate } from '@/protogen/waitroom.pb';
import { SessionStatusMapper } from './session-status.mapper';
import { PositionUpdateRespDto } from '../dtos/resp';

export class StreamPositionMapper {
  static toDto(proto: PositionUpdate): PositionUpdateRespDto {
    console.log(proto);
    return {
      position: Number(proto.position),
      queueLength: Number(proto.queueLength),
      status: SessionStatusMapper.toEnum(proto.status),
      checkoutUrl: proto.checkoutUrl,
      checkoutToken: proto.checkoutToken,
      updatedAt: new Date(proto.updatedAt),
    };
  }
}
