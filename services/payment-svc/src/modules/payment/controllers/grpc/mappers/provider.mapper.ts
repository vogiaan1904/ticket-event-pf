import { PaymentProvider as ProtoPaymentProvider } from '@protogen/payment.pb';
import { PaymentProvider } from '../../../enums/provider.enum';

export class PaymentProviderMapper {
  private static protoToPrismaMap = new Map<ProtoPaymentProvider, PaymentProvider>([
    [ProtoPaymentProvider.ZALOPAY, PaymentProvider.ZALOPAY],
    [ProtoPaymentProvider.PAYOS, PaymentProvider.PAYOS],
    [ProtoPaymentProvider.VNPAY, PaymentProvider.VNPAY],
  ]);

  private static prismaToProtoMap = new Map<PaymentProvider, ProtoPaymentProvider>([
    [PaymentProvider.ZALOPAY, ProtoPaymentProvider.ZALOPAY],
    [PaymentProvider.PAYOS, ProtoPaymentProvider.PAYOS],
    [PaymentProvider.VNPAY, ProtoPaymentProvider.VNPAY],
  ]);

  static toProto(prismaProvider: PaymentProvider): ProtoPaymentProvider {
    const protoProvider = this.prismaToProtoMap.get(prismaProvider);
    if (protoProvider === undefined) {
      throw new Error(`Unknown Prisma PaymentProvider: ${prismaProvider}`);
    }
    return protoProvider;
  }

  static toPrisma(protoProvider: ProtoPaymentProvider): PaymentProvider {
    const prismaProvider = this.protoToPrismaMap.get(protoProvider);
    if (!prismaProvider) {
      throw new Error(`Unknown Proto PaymentProvider: ${protoProvider}`);
    }
    return prismaProvider;
  }
}
