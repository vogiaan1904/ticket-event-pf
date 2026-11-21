import { EventRoleType as ProtoEventRoleType } from '@/protogen/event.pb';
import { EventRoleType as PrismaEventRoleType } from '@prisma/client';

export class EventRoleTypeMapper {
  private static protoToPrismaMap = new Map<ProtoEventRoleType, PrismaEventRoleType>([
    [ProtoEventRoleType.EVENT_ROLE_TYPE_ADMIN, PrismaEventRoleType.ADMIN],
    [ProtoEventRoleType.EVENT_ROLE_TYPE_EDITOR, PrismaEventRoleType.EDITOR],
    [ProtoEventRoleType.EVENT_ROLE_TYPE_VIEWER, PrismaEventRoleType.VIEWER],
  ]);

  private static prismaToProtoMap = new Map<PrismaEventRoleType, ProtoEventRoleType>([
    [PrismaEventRoleType.ADMIN, ProtoEventRoleType.EVENT_ROLE_TYPE_ADMIN],
    [PrismaEventRoleType.EDITOR, ProtoEventRoleType.EVENT_ROLE_TYPE_EDITOR],
    [PrismaEventRoleType.VIEWER, ProtoEventRoleType.EVENT_ROLE_TYPE_VIEWER],
  ]);

  static toProto(prismaRoleType: PrismaEventRoleType): ProtoEventRoleType {
    const protoRoleType = this.prismaToProtoMap.get(prismaRoleType);
    if (!protoRoleType) {
      throw new Error(`Unknown Prisma EventRoleType: ${prismaRoleType}`);
    }
    return protoRoleType;
  }

  static toPrisma(protoRoleType: ProtoEventRoleType): PrismaEventRoleType {
    const prismaRoleType = this.protoToPrismaMap.get(protoRoleType);
    if (!prismaRoleType) {
      throw new Error(`Unknown Proto EventRoleType: ${protoRoleType}`);
    }
    return prismaRoleType;
  }
}
