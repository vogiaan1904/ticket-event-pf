import { getPrismaClient } from '@/common/database/prisma';
import { logger } from '@/common/logger';
import { EventType, PaymentCompletedEvent } from '@/common/types/event.types';
import { PaymentProvider } from '@/common/types/payment.types';
import { handleError, PaymentProviderError, ValidationError } from '@/common/utils/error-handler';
import { PaymentStatus } from '@prisma/client';
import { APIGatewayProxyEvent, APIGatewayProxyResult } from 'aws-lambda';
import { PaymentGatewayInterface } from '../gateways/gateway.interface';
import { PayOSGateway } from '../gateways/payos/payos.gateway';
import { ZalopayGateway } from '../gateways/zalopay/zalopay.gateway';

const detectProvider = (event: APIGatewayProxyEvent): PaymentProvider => {
  const path = event.path || '';

  if (path.includes('/zalopay') || path.includes('/zalo-pay')) {
    return PaymentProvider.ZALOPAY;
  }

  if (path.includes('/payos') || path.includes('/pay-os')) {
    return PaymentProvider.PAYOS;
  }

  const body = JSON.parse(event.body || '{}');

  if (body.data && body.mac && body.type !== undefined) {
    return PaymentProvider.ZALOPAY;
  }

  if (body.code && body.desc && body.data && body.signature) {
    return PaymentProvider.PAYOS;
  }

  throw new ValidationError('Unable to detect payment provider from request');
};

const getGateway = (provider: PaymentProvider): PaymentGatewayInterface => {
  switch (provider) {
    case PaymentProvider.ZALOPAY:
      return new ZalopayGateway();
    case PaymentProvider.PAYOS:
      return new PayOSGateway();
    default:
      throw new ValidationError(`Unsupported payment provider: ${provider}`);
  }
};

const extractOrderCode = (provider: PaymentProvider, appTransId: string): string => {
  if (provider === PaymentProvider.ZALOPAY) {
    const parts = appTransId.split('_');
    return parts.length > 1 ? parts[1] : appTransId;
  }

  return appTransId;
};

export const handleWebhook = async (
  event: APIGatewayProxyEvent,
): Promise<APIGatewayProxyResult> => {
  const requestId = event.requestContext.requestId;

  try {
    if (!event.body) {
      throw new ValidationError('Request body is required');
    }

    const body = JSON.parse(event.body);

    const provider = detectProvider(event);

    const gateway = getGateway(provider);

    const callbackResult = await gateway.handleCallback(body);

    if (!callbackResult.success) {
      logger.warn('Callback validation failed', {
        provider,
        providerTransactionId: callbackResult.providerTransactionId,
        requestId,
      });

      return {
        statusCode: 200,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(callbackResult.response),
      };
    }

    let orderCode: string;

    if (provider === PaymentProvider.ZALOPAY) {
      const callbackData = JSON.parse(body.data);
      orderCode = extractOrderCode(provider, callbackData.app_trans_id);
    } else if (provider === PaymentProvider.PAYOS) {
      const numericOrderCode = body.data.orderCode;

      const prisma = getPrismaClient();
      const payment = await prisma.payment.findFirst({
        where: {
          providerTransactionId: callbackResult.providerTransactionId,
        },
      });

      if (!payment) {
        throw new ValidationError(
          `Payment not found for transaction ${callbackResult.providerTransactionId}`,
        );
      }

      orderCode = payment.orderCode;
    } else {
      throw new ValidationError(`Unsupported provider: ${provider}`);
    }

    const prisma = getPrismaClient();

    await prisma.$transaction(async (tx: any) => {
      const payment = await tx.payment.findUnique({
        where: { orderCode },
      });

      if (!payment) {
        throw new ValidationError(`Payment not found for order ${orderCode}`);
      }

      if (payment.status === PaymentStatus.COMPLETED) {
        logger.warn('Payment already completed, skipping update', {
          orderCode,
          paymentId: payment.id,
          requestId,
        });
        return;
      }

      await tx.payment.update({
        where: { id: payment.id },
        data: {
          status: PaymentStatus.COMPLETED,
          providerTransactionId:
            callbackResult.providerTransactionId || payment.providerTransactionId,
          completedAt: new Date(),
          updatedAt: new Date(),
        },
      });

      const eventPayload: PaymentCompletedEvent = {
        payment_id: payment.id,
        order_code: payment.orderCode,
        amount_cents: payment.amountCents,
        currency: payment.currency,
        provider: payment.provider,
        transaction_id: callbackResult.providerTransactionId || payment.providerTransactionId || '',
        completed_at: (payment.completedAt || new Date()).toISOString(),
      };

      await tx.outbox.create({
        data: {
          aggregateId: payment.id,
          aggregateType: 'payment',
          eventType: EventType.PAYMENT_COMPLETED,
          payload: eventPayload as any,
          published: false,
          retryCount: 0,
        },
      });

      logger.info('Payment completed', {
        orderCode,
        requestId,
      });
    });

    return {
      statusCode: 200,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(callbackResult.response),
    };
  } catch (error) {
    logger.error('Webhook handler error', {
      error: error instanceof Error ? error.message : String(error),
      requestId,
    });

    if (error instanceof PaymentProviderError) {
      return {
        statusCode: 200,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          return_code: -1,
          return_message: error.message,
        }),
      };
    }

    return handleError(error, { requestId });
  }
};
