import { SessionStatus as ProtoSessionStatus } from '@/protogen/waitroom.pb';
import { SessionStatus } from '../enums';

export class SessionStatusMapper {
  private static enumToProtoMap = new Map<SessionStatus, ProtoSessionStatus>([
    [SessionStatus.UNSPECIFIED, ProtoSessionStatus.SESSION_STATUS_UNSPECIFIED],
    [SessionStatus.QUEUED, ProtoSessionStatus.SESSION_STATUS_QUEUED],
    [SessionStatus.PROCESSING, ProtoSessionStatus.SESSION_STATUS_PROCESSING],
    [SessionStatus.READY, ProtoSessionStatus.SESSION_STATUS_READY],
    [SessionStatus.ACTIVE, ProtoSessionStatus.SESSION_STATUS_ACTIVE],
    [SessionStatus.COMPLETED, ProtoSessionStatus.SESSION_STATUS_COMPLETED],
    [SessionStatus.FAILED, ProtoSessionStatus.SESSION_STATUS_FAILED],
    [SessionStatus.EXPIRED, ProtoSessionStatus.SESSION_STATUS_EXPIRED],
    [SessionStatus.CANCELLED, ProtoSessionStatus.SESSION_STATUS_CANCELLED],
  ]);

  private static protoToEnumMap = new Map<ProtoSessionStatus, SessionStatus>([
    [ProtoSessionStatus.SESSION_STATUS_UNSPECIFIED, SessionStatus.UNSPECIFIED],
    [ProtoSessionStatus.SESSION_STATUS_QUEUED, SessionStatus.QUEUED],
    [ProtoSessionStatus.SESSION_STATUS_PROCESSING, SessionStatus.PROCESSING],
    [ProtoSessionStatus.SESSION_STATUS_READY, SessionStatus.READY],
    [ProtoSessionStatus.SESSION_STATUS_ACTIVE, SessionStatus.ACTIVE],
    [ProtoSessionStatus.SESSION_STATUS_COMPLETED, SessionStatus.COMPLETED],
    [ProtoSessionStatus.SESSION_STATUS_FAILED, SessionStatus.FAILED],
    [ProtoSessionStatus.SESSION_STATUS_EXPIRED, SessionStatus.EXPIRED],
    [ProtoSessionStatus.SESSION_STATUS_CANCELLED, SessionStatus.CANCELLED],
  ]);

  static toProto(status: SessionStatus): ProtoSessionStatus {
    const protoStatus = this.enumToProtoMap.get(status);
    if (!protoStatus) {
      throw new Error(`Unknown SessionStatus: ${status}`);
    }
    return protoStatus;
  }

  static toEnum(protoStatus: ProtoSessionStatus): SessionStatus {
    const status = this.protoToEnumMap.get(protoStatus);
    if (!status) {
      throw new Error(`Unknown ProtoSessionStatus: ${protoStatus}`);
    }
    return status;
  }
}
