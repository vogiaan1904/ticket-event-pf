import { PaymentStatus } from '@prisma/client';
import { PaymentStatus as ProtoStatus } from '@protogen/payment.pb';

export class PaymentStatusMapper {
  private static protoToPrismaMap = new Map<ProtoStatus, PaymentStatus>([
    [ProtoStatus.PENDING, PaymentStatus.PENDING],
    [ProtoStatus.COMPLETED, PaymentStatus.COMPLETED],
    [ProtoStatus.FAILED, PaymentStatus.FAILED],
  ]);

  private static prismaToProtoMap = new Map<PaymentStatus, ProtoStatus>([
    [PaymentStatus.PENDING, ProtoStatus.PENDING],
    [PaymentStatus.COMPLETED, ProtoStatus.COMPLETED],
    [PaymentStatus.FAILED, ProtoStatus.FAILED],
  ]);

  static toProto(prismaStatus: PaymentStatus): ProtoStatus {
    const protoStatus = this.prismaToProtoMap.get(prismaStatus);
    if (!protoStatus) {
      throw new Error(`Unknown Prisma PaymentStatus: ${prismaStatus}`);
    }
    return protoStatus;
  }

  static toPrisma(protoStatus: ProtoStatus): PaymentStatus {
    const prismaStatus = this.protoToPrismaMap.get(protoStatus);
    if (!prismaStatus) {
      throw new Error(`Unknown Proto PaymentStatus: ${protoStatus}`);
    }
    return prismaStatus;
  }
}
