import { getDb } from '@/common/db/kysely';
import { logger } from '@/common/logger';
import { randomUUID } from 'crypto';
import { EventType, PaymentCompletedEvent } from '@/common/types/event.types';
import { PaymentProvider } from '@/common/types/payment.types';
import { handleError, PaymentProviderError, ValidationError } from '@/common/utils/error-handler';
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

// Returns true if THIS call performed the transition (and wrote the outbox row).
// Concurrent duplicates hit the `status = 'PENDING'` guard, match 0 rows, and no-op.
export const completePaymentAndEnqueue = async (
  orderCode: string,
  providerTransactionId: string | undefined,
): Promise<boolean> =>
  getDb().transaction().execute(async (trx) => {
    const now = new Date();
    const updated = await trx
      .updateTable('payments')
      .set({
        status: 'COMPLETED',
        completedAt: now,
        updatedAt: now,
        ...(providerTransactionId ? { providerTransactionId } : {}),
      })
      .where('orderCode', '=', orderCode)
      .where('status', '=', 'PENDING')
      .returning(['id', 'orderCode', 'amountCents', 'currency', 'provider', 'providerTransactionId'])
      .executeTakeFirst();

    if (!updated) return false; // already completed OR not found -> idempotent no-op

    const payload: PaymentCompletedEvent = {
      payment_id: updated.id,
      order_code: updated.orderCode,
      amount_cents: updated.amountCents,
      currency: updated.currency,
      provider: updated.provider,
      transaction_id: updated.providerTransactionId ?? '',
      completed_at: now.toISOString(),
    };

    await trx
      .insertInto('outbox')
      .values({
        id: randomUUID(), // no DB-level default on outbox.id
        aggregateId: updated.id,
        aggregateType: 'payment',
        eventType: EventType.PAYMENT_COMPLETED,
        payload: JSON.stringify(payload),
        retryCount: 0,
      })
      .execute();

    return true;
  });

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
      const payment = await getDb()
        .selectFrom('payments')
        .select('orderCode')
        .where('providerTransactionId', '=', callbackResult.providerTransactionId!)
        .executeTakeFirst();
      if (!payment) throw new ValidationError(`Payment not found for transaction ${callbackResult.providerTransactionId}`);
      orderCode = payment.orderCode;
    } else {
      throw new ValidationError(`Unsupported provider: ${provider}`);
    }

    const didComplete = await completePaymentAndEnqueue(orderCode, callbackResult.providerTransactionId);
    if (!didComplete) {
      logger.warn('Payment already completed or not found, skipping', { orderCode, requestId });
    } else {
      logger.info('Payment completed', { orderCode, requestId });
    }

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
