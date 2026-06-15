import { Controller } from '@nestjs/common';

@Controller('webhook')
export class PaymentController {
  // All webhook endpoints have been migrated to AWS Lambda
  // See: lambdas/payment-webhook-handler
}
