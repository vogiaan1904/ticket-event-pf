/**
 * Tests for webhook handler
 */

import { handleWebhook } from '../handlers/webhook.handler';
import { getDb, closeDb } from '@/common/db/kysely';
import { EventType } from '@/common/types/event.types';
import { createMockAPIGatewayEvent } from '../../__tests__/utils/mock-helpers';

jest.mock('@/common/logger');
jest.mock('../gateways/zalopay/zalopay.gateway');
jest.mock('../gateways/payos/payos.gateway');

afterAll(async () => {
  await closeDb();
});

beforeEach(async () => {
  jest.clearAllMocks();
  const db = getDb();
  await db.deleteFrom('outbox').execute();
  await db.deleteFrom('payments').execute();
});

interface SeedOverrides {
  id?: string;
  orderCode?: string;
  providerTransactionId?: string;
  status?: 'PENDING' | 'COMPLETED' | 'CANCELLED' | 'FAILED';
}

const seedPayment = ({
  id = 'payment-123',
  orderCode = 'ORDER123',
  providerTransactionId = 'tx-1',
  status = 'PENDING',
}: SeedOverrides = {}) =>
  getDb()
    .insertInto('payments')
    .values({
      id,
      orderCode,
      amountCents: 100000,
      currency: 'VND',
      provider: 'zalopay',
      providerTransactionId,
      idempotencyKey: `idem-${id}`,
      redirectUrl: 'https://example.com/redirect',
      paymentUrl: 'https://example.com/pay',
      status,
      updatedAt: new Date(),
    })
    .execute();

describe('Webhook Handler', () => {
  describe('Provider Detection', () => {
    it('should detect ZaloPay from path', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: JSON.stringify({
          data: '{}',
          mac: 'test_mac',
          type: 1,
        }),
      });

      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: false,
        response: { return_code: -1, return_message: 'Invalid mac' },
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      expect(ZalopayGateway.prototype.handleCallback).toHaveBeenCalled();
    });

    it('should detect PayOS from path', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/payos',
        body: JSON.stringify({
          code: '00',
          desc: 'Success',
          data: { orderCode: 123456 },
          signature: 'test_signature',
        }),
      });

      const { PayOSGateway } = require('../gateways/payos/payos.gateway');
      PayOSGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: false,
        response: { error: -1, message: 'Invalid signature' },
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      expect(PayOSGateway.prototype.handleCallback).toHaveBeenCalled();
    });

    it('should detect ZaloPay from body structure', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook',
        body: JSON.stringify({
          data: JSON.stringify({ app_trans_id: '250101_ORDER123' }),
          mac: 'test_mac',
          type: 1,
        }),
      });

      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: false,
        response: { return_code: -1, return_message: 'Invalid mac' },
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      expect(ZalopayGateway.prototype.handleCallback).toHaveBeenCalled();
    });

    it('should throw error if provider cannot be detected', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook',
        body: JSON.stringify({
          unknown: 'field',
        }),
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(400);
      const body = JSON.parse(result.body);
      expect(body.error).toBe('ValidationError');
    });
  });

  describe('ZaloPay Webhook Processing', () => {
    it('should successfully process a valid ZaloPay callback and complete the payment', async () => {
      await seedPayment({ orderCode: 'ORDER123' });

      const callbackData = { app_trans_id: '250101_ORDER123', amount: 100000 };
      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: JSON.stringify({ data: JSON.stringify(callbackData), mac: 'valid_mac', type: 1 }),
      });

      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { return_code: 1, return_message: 'Success' },
        providerTransactionId: '12345678',
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      const body = JSON.parse(result.body);
      expect(body.return_code).toBe(1);

      const payment = await getDb()
        .selectFrom('payments')
        .select(['status', 'providerTransactionId'])
        .where('orderCode', '=', 'ORDER123')
        .executeTakeFirst();
      expect(payment?.status).toBe('COMPLETED');
      expect(payment?.providerTransactionId).toBe('12345678');

      const outboxRows = await getDb().selectFrom('outbox').selectAll().execute();
      expect(outboxRows).toHaveLength(1);
      expect(outboxRows[0].eventType).toBe(EventType.PAYMENT_COMPLETED);
    });

    it('should return error response for invalid ZaloPay signature', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: JSON.stringify({
          data: JSON.stringify({ app_trans_id: '250101_ORDER123' }),
          mac: 'invalid_mac',
          type: 1,
        }),
      });

      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: false,
        response: { return_code: -1, return_message: 'Invalid mac' },
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      const body = JSON.parse(result.body);
      expect(body.return_code).toBe(-1);

      const outboxRows = await getDb().selectFrom('outbox').selectAll().execute();
      expect(outboxRows).toHaveLength(0);
    });

    it('should skip update and not duplicate the outbox event if payment already completed', async () => {
      await seedPayment({ orderCode: 'ORDER123', status: 'COMPLETED' });

      const callbackData = { app_trans_id: '250101_ORDER123', amount: 100000 };
      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: JSON.stringify({ data: JSON.stringify(callbackData), mac: 'valid_mac', type: 1 }),
      });

      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { return_code: 1, return_message: 'Success' },
        providerTransactionId: '12345678',
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);

      const outboxRows = await getDb().selectFrom('outbox').selectAll().execute();
      expect(outboxRows).toHaveLength(0);
    });
  });

  describe('PayOS Webhook Processing', () => {
    it('should successfully process a valid PayOS callback', async () => {
      await seedPayment({ orderCode: 'ORDER456', providerTransactionId: 'payos-link-123' });

      const webhookBody = {
        code: '00',
        desc: 'Success',
        data: { orderCode: 25010100000123, amount: 200000, paymentLinkId: 'payos-link-123' },
        signature: 'valid_signature',
      };
      const event = createMockAPIGatewayEvent({
        path: '/webhook/payos',
        body: JSON.stringify(webhookBody),
      });

      const { PayOSGateway } = require('../gateways/payos/payos.gateway');
      PayOSGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { error: 0, message: 'Success' },
        providerTransactionId: 'payos-link-123',
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      const body = JSON.parse(result.body);
      expect(body.error).toBe(0);

      const payment = await getDb()
        .selectFrom('payments')
        .select('status')
        .where('orderCode', '=', 'ORDER456')
        .executeTakeFirst();
      expect(payment?.status).toBe('COMPLETED');
    });

    it('should return error response for invalid PayOS signature', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/payos',
        body: JSON.stringify({
          code: '00',
          desc: 'Success',
          data: { orderCode: 123456 },
          signature: 'invalid_signature',
        }),
      });

      const { PayOSGateway } = require('../gateways/payos/payos.gateway');
      PayOSGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: false,
        response: { error: -1, message: 'Invalid signature' },
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      const body = JSON.parse(result.body);
      expect(body.error).toBe(-1);
    });

    it('should handle payment not found error', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/payos',
        body: JSON.stringify({
          code: '00',
          desc: 'Success',
          data: { orderCode: 123456, paymentLinkId: 'unknown-link' },
          signature: 'valid_signature',
        }),
      });

      const { PayOSGateway } = require('../gateways/payos/payos.gateway');
      PayOSGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { error: 0, message: 'Success' },
        providerTransactionId: 'unknown-link',
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(400);
      const body = JSON.parse(result.body);
      expect(body.error).toBe('ValidationError');
      expect(body.message).toContain('Payment not found');
    });
  });

  describe('Error Handling', () => {
    it('should handle missing request body', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: null,
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(400);
      const body = JSON.parse(result.body);
      expect(body.error).toBe('ValidationError');
      expect(body.message).toContain('Request body is required');
    });

    it('should handle invalid JSON body', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: 'invalid json',
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(500);
      const body = JSON.parse(result.body);
      expect(body.error).toBe('SyntaxError');
    });
  });
});
