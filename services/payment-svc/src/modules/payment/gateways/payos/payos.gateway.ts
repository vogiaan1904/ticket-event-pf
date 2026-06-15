import { AppConfigService } from '@/shared/services/config.service';
import { Injectable, InternalServerErrorException, Logger } from '@nestjs/common';
import { PayOS } from '@payos/node';
import {
  CreatePaymentLinkInput,
  CreatePaymentLinkOutput,
  HandleCallbackOutput,
  PaymentGatewayInterface,
} from '../gateway.interface';
import {
  PayOSCallbackResponse,
  PayOSCreatePaymentResponse,
  PayOSPaymentRequestBody,
  PayOSWebhookBody,
  PayOSWebhookData,
} from './payos.interface';

@Injectable()
export class PayOSGateway implements PaymentGatewayInterface {
  private readonly logger = new Logger(PayOSGateway.name);
  private readonly ORDER_TIMEOUT_SECONDS = 300; // 5 minutes
  private readonly payOS: any; // PayOS instance from @payos/node SDK

  constructor(private readonly configService: AppConfigService) {
    const { clientId, apiKey, checksumKey } = this.configService.payosConfig;

    this.payOS = new PayOS({
      clientId,
      apiKey,
      checksumKey,
    });
  }

  /**
   * Convert string orderCode to numeric PayOS orderCode (deterministic)
   * Format: TB-{EVENT}-{YYYYMMDD}-{8_CHAR_ALPHANUMERIC}
   * Example: TB-TSE24-20251008-A3B7K9M2 -> 2025100800XXXXX
   */
  private buildPayOSOrderCode(orderCode: string): number {
    const dateMatch = orderCode.match(/(\d{8})/);
    if (!dateMatch) {
      throw new Error(`Invalid order code format - missing date: ${orderCode}`);
    }
    const dateNum = parseInt(dateMatch[1], 10);

    const parts = orderCode.split('-');
    const alphanumeric = parts[parts.length - 1];
    if (alphanumeric.length < 5) {
      throw new Error(`Invalid order code format - alphanumeric too short: ${orderCode}`);
    }
    const last5 = alphanumeric.slice(-5).toUpperCase();

    const alphaNum = parseInt(last5, 36);

    if (isNaN(alphaNum)) {
      throw new Error(`Invalid alphanumeric code: ${last5}`);
    }

    const payosOrderCode = dateNum * 100000000 + alphaNum;

    this.logger.debug(`Order code mapping: ${orderCode} -> ${payosOrderCode}`);

    return payosOrderCode;
  }

  /**
   * Reverse lookup: PayOS numeric code -> original orderCode
   *
   * We can't fully reconstruct the original orderCode from just the number
   * (we lose EVENT_CODE and first 3 chars of alphanumeric).
   *
   * Solution: Query database using providerTransactionId
   * The payment record contains both:
   * - orderCode (original string)
   * - providerTransactionId (PayOS numeric code as string)
   */
  private getOrderCodeFromPayOSOrderCode(payosOrderCode: number): string {
    // NOTE: This is called from webhook handler
    // PayOS sends us the numeric orderCode in the webhook
    // We need to find the original orderCode from our database

    // Return PayOS orderCode as string
    // The payment service will use this to look up the payment record
    // by providerTransactionId and get the actual orderCode
    return payosOrderCode.toString();
  }

  private initPayOSCallbackRes(success: boolean, message?: string): PayOSCallbackResponse {
    return {
      success,
      message,
    };
  }

  private buildPaymentRequestBody(input: CreatePaymentLinkInput): PayOSPaymentRequestBody {
    const payosOrderCode = this.buildPayOSOrderCode(input.orderCode);

    let cancelUrl = input.redirectUrl;
    let returnUrl = input.redirectUrl;

    // Add orderCode to URLs
    if (cancelUrl.includes('?')) {
      cancelUrl += `&bookingCode=${input.orderCode}&status=cancelled`;
      returnUrl += `&bookingCode=${input.orderCode}&status=success`;
    } else {
      cancelUrl += `?bookingCode=${input.orderCode}&status=cancelled`;
      returnUrl += `?bookingCode=${input.orderCode}&status=success`;
    }

    const expiredAt =
      Math.floor(Date.now() / 1000) + (input.timeoutSeconds || this.ORDER_TIMEOUT_SECONDS);

    const body: PayOSPaymentRequestBody = {
      orderCode: payosOrderCode,
      amount: input.amount,
      description: `Payment for order ${input.orderCode}`,
      items: [
        {
          name: `Order ${input.orderCode}`,
          quantity: 1,
          price: input.amount,
        },
      ],
      cancelUrl,
      returnUrl,
      expiredAt,
    };

    return body;
  }

  async createPaymentLink(input: CreatePaymentLinkInput): Promise<CreatePaymentLinkOutput> {
    try {
      const body = this.buildPaymentRequestBody(input);
      const response: PayOSCreatePaymentResponse = await this.payOS.createPaymentLink(body);

      if (!response.checkoutUrl) {
        this.logger.error('PayOS response missing checkoutUrl', response);
        throw new InternalServerErrorException('Failed to create PayOS payment link');
      }

      return {
        url: response.checkoutUrl,
        transactionId: response.paymentLinkId,
      };
    } catch (error) {
      this.logger.error('Error creating PayOS payment link:', error);
      throw new InternalServerErrorException('There was an error with PayOS');
    }
  }

  async handleCallback(callbackBody: PayOSWebhookBody): Promise<HandleCallbackOutput> {
    try {
      let webhookData: PayOSWebhookData;
      try {
        webhookData = this.payOS.verifyPaymentWebhookData(callbackBody);
      } catch (error) {
        this.logger.warn('Invalid PayOS webhook signature', error);
        return {
          success: false,
          response: this.initPayOSCallbackRes(false, 'Invalid signature'),
        };
      }

      const isSuccess = webhookData.code === '00';

      if (!isSuccess) {
        this.logger.warn(`PayOS payment failed with code: ${webhookData.code}`);
      }

      const paymentLinkId = webhookData.paymentLinkId;

      this.logger.log(
        `PayOS callback: paymentLinkId=${paymentLinkId}, orderCode=${webhookData.orderCode}, success=${isSuccess}`,
      );

      return {
        success: isSuccess,
        response: this.initPayOSCallbackRes(true, 'Success'),
        providerTransactionId: paymentLinkId,
      };
    } catch (error) {
      this.logger.error('Error processing PayOS callback:', error);
      return {
        success: false,
        response: this.initPayOSCallbackRes(false, 'Processing error'),
      };
    }
  }

  /**
   * Optional: Get payment information by orderCode
   * Useful for checking payment status
   */
  async getPaymentInfo(orderCode: string): Promise<any> {
    try {
      const payosOrderCode = this.buildPayOSOrderCode(orderCode);
      const paymentInfo = await this.payOS.getPaymentLinkInformation(payosOrderCode);
      return paymentInfo;
    } catch (error) {
      this.logger.error(`Error getting PayOS payment info for order ${orderCode}:`, error);
      throw error;
    }
  }

  /**
   * Optional: Cancel payment
   */
  async cancelPayment(orderCode: string, reason?: string): Promise<any> {
    try {
      const payosOrderCode = this.buildPayOSOrderCode(orderCode);
      const result = await this.payOS.cancelPaymentLink(
        payosOrderCode,
        reason || 'Order cancelled by user',
      );
      this.logger.log(`PayOS payment cancelled for order: ${orderCode}`);
      return result;
    } catch (error) {
      this.logger.error(`Error cancelling PayOS payment for order ${orderCode}:`, error);
      throw error;
    }
  }
}
