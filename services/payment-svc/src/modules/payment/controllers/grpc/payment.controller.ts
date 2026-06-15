import { Controller } from '@nestjs/common';
import { PaymentService } from '../../payment.service';
import { LoggerService } from '@/shared/services/logger.service';
import {
  CreatePaymentIntentResponse,
  GetPaymentUrlByIdempotencyKeyResponse,
  PAYMENT_SERVICE_NAME,
} from '@/protogen/payment.pb';
import { CreatePaymentIntentDto } from './dto/create-intent.dto';
import { GrpcMethod } from '@nestjs/microservices';
import { GetUrlDto } from './dto';
import { PaymentStatusMapper } from './mappers/status.mapper';

@Controller()
export class GrpcPaymentController {
  constructor(
    private readonly paymentService: PaymentService,
    private readonly logger: LoggerService,
  ) {
    logger.setContext(GrpcPaymentController.name);
  }

  @GrpcMethod(PAYMENT_SERVICE_NAME, 'createPaymentIntent')
  async createIntent(dto: CreatePaymentIntentDto): Promise<CreatePaymentIntentResponse> {
    this.logger.info(`GrpcPaymentController.createPaymentIntent called.`);
    const url = await this.paymentService.createPaymentIntent(dto.toServiceDto());
    this.logger.info(`GrpcPaymentController.createPaymentIntent completed.`);

    return { paymentUrl: url };
  }

  @GrpcMethod(PAYMENT_SERVICE_NAME, 'getPaymentUrlByIdempotencyKey')
  async getPaymentUrlByIdempotencyKey(
    dto: GetUrlDto,
  ): Promise<GetPaymentUrlByIdempotencyKeyResponse> {
    this.logger.info(`GrpcPaymentController.getPaymentUrlByIdempotencyKey called.`);
    const payment = await this.paymentService.findByIdempotencyKey(dto.idempotencyKey);
    this.logger.info(`GrpcPaymentController.getPaymentUrlByIdempotencyKey completed.`);

    return {
      paymentUrl: payment.paymentUrl,
      status: PaymentStatusMapper.toProto(payment.status),
    };
  }
}
