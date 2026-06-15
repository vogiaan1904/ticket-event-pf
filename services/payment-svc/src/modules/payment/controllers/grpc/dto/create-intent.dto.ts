import { CreatePaymentIntentDto as ServiceCreatePaymentIntentDto } from '@/modules/payment/dto';
import { CreatePaymentIntentRequest, PaymentProvider } from '@/protogen/payment.pb';
import { IsEnum, IsNotEmpty, IsNumber, IsPositive, IsString, IsUrl } from 'class-validator';
import { CurrencyMapper } from '../mappers/currency.mapper';
import { PaymentProviderMapper } from '../mappers/provider.mapper';
import { Type } from 'class-transformer';

export class CreatePaymentIntentDto implements CreatePaymentIntentRequest {
  toServiceDto(): ServiceCreatePaymentIntentDto {
    return {
      amountCents: this.amountCents,
      currency: CurrencyMapper.toPrisma(this.currency),
      orderCode: this.orderCode,
      idempotencyKey: this.idempotencyKey,
      provider: PaymentProviderMapper.toPrisma(this.provider),
      redirectUrl: this.redirectUrl,
      transactionId: '',
      timeoutSeconds: this.timeoutSeconds,
      paymentUrl: '',
    };
  }

  @IsNotEmpty()
  @IsString()
  orderCode: string;

  @IsNotEmpty()
  @IsNumber()
  @Type(() => Number)
  amountCents: number;

  @IsNotEmpty()
  @IsString()
  currency: string;

  @IsNotEmpty()
  @IsEnum(PaymentProvider)
  @Type(() => Number)
  provider: PaymentProvider;

  @IsNotEmpty()
  @IsString()
  idempotencyKey: string;

  @IsNotEmpty()
  @IsUrl()
  redirectUrl: string;

  @IsNotEmpty()
  @IsNumber()
  @IsPositive()
  @Type(() => Number)
  timeoutSeconds: number;
}
