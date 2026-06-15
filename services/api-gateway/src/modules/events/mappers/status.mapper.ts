import { EventStatus as ProtoEventStatus } from '@/protogen/event.pb';
import { EventStatus } from '../enums';

export class StatusMapper {
  private static enumToProtoMap = new Map<EventStatus, ProtoEventStatus>([
    [EventStatus.DRAFT, ProtoEventStatus.EVENT_STATUS_DRAFT],
    [EventStatus.CONFIGURED, ProtoEventStatus.EVENT_STATUS_CONFIGURED],
    [EventStatus.APPROVED, ProtoEventStatus.EVENT_STATUS_APPROVED],
    [EventStatus.PUBLISHED, ProtoEventStatus.EVENT_STATUS_PUBLISHED],
    [EventStatus.CANCELLED, ProtoEventStatus.EVENT_STATUS_CANCELLED],
  ]);

  private static protoToEnumMap = new Map<ProtoEventStatus, EventStatus>([
    [ProtoEventStatus.EVENT_STATUS_DRAFT, EventStatus.DRAFT],
    [ProtoEventStatus.EVENT_STATUS_CONFIGURED, EventStatus.CONFIGURED],
    [ProtoEventStatus.EVENT_STATUS_APPROVED, EventStatus.APPROVED],
    [ProtoEventStatus.EVENT_STATUS_PUBLISHED, EventStatus.PUBLISHED],
    [ProtoEventStatus.EVENT_STATUS_CANCELLED, EventStatus.CANCELLED],
  ]);

  static toProto(status: EventStatus): ProtoEventStatus {
    const protoStatus = this.enumToProtoMap.get(status);
    if (!protoStatus) {
      throw new Error(`Unknown EventStatus: ${status}`);
    }
    return protoStatus;
  }

  static toEnum(protoStatus: ProtoEventStatus): EventStatus {
    const status = this.protoToEnumMap.get(protoStatus);
    if (!status) {
      throw new Error(`Unknown ProtoEventStatus: ${protoStatus}`);
    }
    return status;
  }
}
