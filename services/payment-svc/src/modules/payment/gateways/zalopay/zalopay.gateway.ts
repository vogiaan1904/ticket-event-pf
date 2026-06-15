import { AppConfigService } from '@/shared/services/config.service';
import { HttpService } from '@nestjs/axios';
import { Injectable, InternalServerErrorException, Logger } from '@nestjs/common';
import { AxiosResponse } from 'axios';
import * as crypto from 'crypto';
import * as dayjs from 'dayjs';
import {
  CreatePaymentLinkInput,
  CreatePaymentLinkOutput,
  HandleCallbackOutput,
  PaymentGatewayInterface,
} from '../gateway.interface';
import {
  ZaloCreatePaymentUrlRequestBody,
  ZaloCreatePaymentUrlResponse,
  ZalopayCallbackBody,
  ZalopayCallbackData,
  ZalopayCallbackResponse,
} from './zalopay.interface';

@Injectable()
export class ZalopayGateWay implements PaymentGatewayInterface {
  private readonly logger = new Logger(ZalopayGateWay.name);
  private readonly ORDER_TIMEOUT_SECONDS = 300; //5 minutes
  private readonly CREATE_ZALOPAY_PAYMENT_LINK_URL = 'https://sb-openapi.zalopay.vn/v2/create';
  private readonly appID: number;
  private readonly key1: string;
  private readonly key2: string;
  private readonly callbackErrorCode = -1;
  private readonly webhookBaseUrl: string;

  constructor(
    private readonly httpService: HttpService,
    private readonly configService: AppConfigService,
  ) {
    this.appID = this.configService.zalopayConfig.appID;
    this.key1 = this.configService.zalopayConfig.key1;
    this.key2 = this.configService.zalopayConfig.key2;
    this.webhookBaseUrl = this.configService.appConfig.webhookBaseUrl;
  }

  private buildZaloPayAppTransId(orderCode: string): string {
    const now = dayjs();
    return `${now.format('YYMMDD')}_${orderCode}`;
  }

  private getOrderCodeFromAppTransId(appTransId: string): string {
    const parts = appTransId.split('_');
    if (parts.length !== 2) {
      throw new Error('Invalid app_trans_id format');
    }
    return parts[1];
  }

  private initZaloPayCallbackRes(code: number, message: string): ZalopayCallbackResponse {
    return {
      return_code: code,
      return_message: message,
    };
  }

  private initZaloPayRequestBody(data: CreatePaymentLinkInput): ZaloCreatePaymentUrlRequestBody {
    const now = dayjs();
    const appTransId = this.buildZaloPayAppTransId(data.orderCode);

    let redirectUrl = data.redirectUrl;
    if (redirectUrl.includes('?')) {
      redirectUrl += `&bookingCode=${data.orderCode}`;
    } else {
      redirectUrl += `?bookingCode=${data.orderCode}`;
    }

    const callbackUrl = this.webhookBaseUrl
      ? `${this.webhookBaseUrl}/webhook/zalopay`
      : 'https://api.ticketbottle.com/webhook/zalopay';

    const body: ZaloCreatePaymentUrlRequestBody = {
      app_id: this.appID * 1,
      app_user: 'TicketBottle',
      app_time: now.valueOf(),
      amount: data.amount,
      app_trans_id: appTransId,
      embed_data: JSON.stringify({
        redirecturl: redirectUrl,
      }),
      expire_duration_seconds: data.timeoutSeconds || this.ORDER_TIMEOUT_SECONDS,
      description: 'Payment for order ' + data.orderCode,
      bank_code: '',
      callback_url: callbackUrl,
      item: JSON.stringify([]),
      mac: '',
    };

    const macInput =
      body.app_id +
      '|' +
      body.app_trans_id +
      '|' +
      body.app_user +
      '|' +
      body.amount +
      '|' +
      body.app_time +
      '|' +
      body.embed_data +
      '|' +
      body.item;
    body.mac = crypto.createHmac('sha256', this.key1).update(macInput).digest('hex');

    return body;
  }

  async createPaymentLink(input: CreatePaymentLinkInput): Promise<CreatePaymentLinkOutput> {
    const body = this.initZaloPayRequestBody(input);
    const res: AxiosResponse = await this.httpService.axiosRef.post(
      this.CREATE_ZALOPAY_PAYMENT_LINK_URL,
      body,
      {
        headers: {
          'Content-Type': 'application/json',
        },
      },
    );

    const resData: ZaloCreatePaymentUrlResponse = res.data;
    if (resData.return_code !== 1) {
      console.log(resData);
      throw new InternalServerErrorException('There was an error with Zalopay');
    }

    return {
      url: resData.order_url,
      transactionId: body.app_trans_id,
    };
  }

  async handleCallback(callbackBody: ZalopayCallbackBody): Promise<HandleCallbackOutput> {
    try {
      const requestMac = crypto
        .createHmac('sha256', this.key2)
        .update(callbackBody.data)
        .digest('hex');

      if (requestMac !== callbackBody.mac) {
        this.logger.warn('Invalid MAC signature in ZaloPay callback');
        return {
          success: false,
          response: this.initZaloPayCallbackRes(this.callbackErrorCode, 'Invalid mac'),
        };
      }

      const transData: ZalopayCallbackData = JSON.parse(callbackBody.data);
      if (callbackBody.type !== 1) {
        this.logger.warn(`Unsupported callback type: ${callbackBody.type}`);
        return {
          success: false,
          response: this.initZaloPayCallbackRes(
            this.callbackErrorCode,
            'Unsupported callback type',
          ),
        };
      }

      // Return app_trans_id as providerTransactionId for database lookup
      const appTransId = transData.app_trans_id;

      this.logger.log(`ZaloPay callback: appTransId=${appTransId}`);

      return {
        success: true,
        response: this.initZaloPayCallbackRes(1, 'Success'),
        providerTransactionId: appTransId,
      };
    } catch (error) {
      this.logger.error('Error processing ZaloPay callback:', error);
      return {
        success: false,
        response: this.initZaloPayCallbackRes(this.callbackErrorCode, 'Processing error'),
      };
    }
  }
}
