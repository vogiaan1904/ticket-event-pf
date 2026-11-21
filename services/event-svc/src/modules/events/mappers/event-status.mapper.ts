import { EventStatus as ProtoEventStatus } from '@/protogen/event.pb';
import { EventStatus as PrismaEventStatus } from '@prisma/client';

export class EventStatusMapper {
  private static protoToPrismaMap = new Map<ProtoEventStatus, PrismaEventStatus>([
    [ProtoEventStatus.EVENT_STATUS_DRAFT, PrismaEventStatus.DRAFT],
    [ProtoEventStatus.EVENT_STATUS_PUBLISHED, PrismaEventStatus.PUBLISHED],
    [ProtoEventStatus.EVENT_STATUS_CANCELLED, PrismaEventStatus.CANCELLED],
    [ProtoEventStatus.EVENT_STATUS_CONFIGURED, PrismaEventStatus.CONFIGURED],
    [ProtoEventStatus.EVENT_STATUS_APPROVED, PrismaEventStatus.APPROVED],
  ]);

  private static prismaToProtoMap = new Map<PrismaEventStatus, ProtoEventStatus>([
    [PrismaEventStatus.DRAFT, ProtoEventStatus.EVENT_STATUS_DRAFT],
    [PrismaEventStatus.PUBLISHED, ProtoEventStatus.EVENT_STATUS_PUBLISHED],
    [PrismaEventStatus.CANCELLED, ProtoEventStatus.EVENT_STATUS_CANCELLED],
    [PrismaEventStatus.CONFIGURED, ProtoEventStatus.EVENT_STATUS_CONFIGURED],
    [PrismaEventStatus.APPROVED, ProtoEventStatus.EVENT_STATUS_APPROVED],
  ]);

  static toProto(prismaStatus: PrismaEventStatus): ProtoEventStatus {
    const protoStatus = this.prismaToProtoMap.get(prismaStatus);
    if (!protoStatus) {
      throw new Error(`Unknown Prisma EventStatus: ${prismaStatus}`);
    }
    return protoStatus;
  }

  static toPrisma(protoStatus: ProtoEventStatus): PrismaEventStatus {
    const prismaStatus = this.protoToPrismaMap.get(protoStatus);
    if (!prismaStatus) {
      throw new Error(`Unknown Proto EventStatus: ${protoStatus}`);
    }
    return prismaStatus;
  }
}
