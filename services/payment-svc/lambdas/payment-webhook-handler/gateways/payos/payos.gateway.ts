import PayOS from '@payos/node';
import { logger } from '@/common/logger';
import { getConfig } from '@/common/config';
import {
  PaymentGatewayInterface,
  CreatePaymentLinkInput,
  CreatePaymentLinkOutput,
  HandleCallbackOutput,
} from '../gateway.interface';
import {
  PayOSCreatePaymentLinkRequestBody,
  PayOSWebhookBody,
  PayOSWebhookData,
} from './payos.interface';

export class PayOSGateway implements PaymentGatewayInterface {
  private readonly payOS: any;
  private readonly clientId: string;
  private readonly apiKey: string;
  private readonly checksumKey: string;

  constructor() {
    const config = getConfig();
    this.clientId = config.paymentProviders.payos.clientId;
    this.apiKey = config.paymentProviders.payos.apiKey;
    this.checksumKey = config.paymentProviders.payos.checksumKey;

    this.payOS = new (PayOS as any)(this.clientId, this.apiKey, this.checksumKey);
  }

  async createPaymentLink(input: CreatePaymentLinkInput): Promise<CreatePaymentLinkOutput> {
    try {
      // Format: date (YYMMDD) * 1e8 + an alphanumeric conversion of orderCode.
      const numericOrderCode = this.convertOrderCodeToNumber(input.orderCode);

      const requestBody: PayOSCreatePaymentLinkRequestBody = {
        orderCode: numericOrderCode,
        amount: input.amount,
        description: `Payment for order ${input.orderCode}`,
        cancelUrl: input.redirectUrl,
        returnUrl: input.redirectUrl,
        expiredAt: Math.floor(Date.now() / 1000) + input.timeoutSeconds,
      };

      const response = await this.payOS.createPaymentLink(requestBody);

      return {
        url: response.checkoutUrl,
        transactionId: response.paymentLinkId,
      };
    } catch (error) {
      logger.error('Failed to create PayOS payment link', {
        orderCode: input.orderCode,
        error: error instanceof Error ? error.message : String(error),
      });
      throw error;
    }
  }

  async handleCallback(body: PayOSWebhookBody): Promise<HandleCallbackOutput> {
    try {
      // verifyPaymentWebhookData checks the signature as a side effect.
      const webhookData: PayOSWebhookData = await this.payOS.verifyPaymentWebhookData(body);

      // PayOS signals success with code '00'.
      const isSuccess = webhookData.code === '00';

      if (!isSuccess) {
        logger.warn('PayOS payment not successful', {
          orderCode: webhookData.orderCode,
          code: webhookData.code,
          description: webhookData.desc,
        });

        return {
          success: false,
          response: this.initPayOSCallbackRes(-1, webhookData.desc || 'Payment failed'),
          providerTransactionId: webhookData.paymentLinkId,
        };
      }

      return {
        success: true,
        response: this.initPayOSCallbackRes(0, 'Success'),
        providerTransactionId: webhookData.paymentLinkId,
      };
    } catch (error) {
      logger.error('Failed to process PayOS webhook', {
        error: error instanceof Error ? error.message : String(error),
      });

      return {
        success: false,
        response: this.initPayOSCallbackRes(-1, 'Invalid signature or webhook data'),
      };
    }
  }

  private convertOrderCodeToNumber(orderCode: string): number {
    const now = new Date();
    const year = now.getFullYear().toString().slice(-2);
    const month = (now.getMonth() + 1).toString().padStart(2, '0');
    const day = now.getDate().toString().padStart(2, '0');
    const datePrefix = parseInt(`${year}${month}${day}`);

    let numericSuffix = 0;

    const numericPart = orderCode.replace(/\D/g, '');
    if (numericPart.length > 0) {
      // Last 8 digits only, so it fits the numeric range.
      numericSuffix = parseInt(numericPart.slice(-8)) % 100000000;
    } else {
      for (let i = 0; i < orderCode.length && i < 8; i++) {
        numericSuffix = (numericSuffix * 10 + orderCode.charCodeAt(i)) % 100000000;
      }
    }

    return datePrefix * 100000000 + numericSuffix;
  }

  private initPayOSCallbackRes(error: number, message: string): any {
    return {
      error,
      message,
      data: null,
    };
  }
}
