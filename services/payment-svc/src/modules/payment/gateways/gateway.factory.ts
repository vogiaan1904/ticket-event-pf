import { Injectable } from '@nestjs/common';
import { PaymentProvider } from '../enums/provider.enum';
import { ZalopayGateWay } from './zalopay/zalopay.gateway';

@Injectable()
export class PaymentGatewayFactory {
  constructor(private readonly zalopayGateWay: ZalopayGateWay) {}

  getGateway(provider: PaymentProvider) {
    switch (provider) {
      case PaymentProvider.ZALOPAY:
        return this.zalopayGateWay;

      default:
        throw new Error(`Unsupported provider: ${provider}`);
    }
  }
}
