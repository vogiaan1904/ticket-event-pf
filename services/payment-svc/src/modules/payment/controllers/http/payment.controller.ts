import { Controller } from '@nestjs/common';

@Controller('webhook')
export class PaymentController {
  // Webhook routes are served by lambdas/payment-webhook-handler.
}
