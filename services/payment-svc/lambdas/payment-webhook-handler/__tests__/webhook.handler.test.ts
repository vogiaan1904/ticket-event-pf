/**
 * Unit tests for webhook handler
 */

import { APIGatewayProxyEvent } from 'aws-lambda';
import { handleWebhook } from '../handlers/webhook.handler';
import { getPrismaClient } from '@/common/database/prisma';
import { PaymentStatus, PaymentProvider } from '@/common/types/payment.types';
import { EventType } from '@/common/types/event.types';
import { createMockAPIGatewayEvent } from '../../__tests__/utils/mock-helpers';
import crypto from 'crypto';

// Mock dependencies
jest.mock('@/common/database/prisma');
jest.mock('@/common/logger');
jest.mock('../gateways/zalopay/zalopay.gateway');
jest.mock('../gateways/payos/payos.gateway');

const mockPrismaClient = {
  payment: {
    findUnique: jest.fn(),
    findFirst: jest.fn(),
    update: jest.fn(),
  },
  outboxEvent: {
    create: jest.fn(),
  },
  $transaction: jest.fn(),
};

(getPrismaClient as jest.Mock).mockReturnValue(mockPrismaClient);

describe('Webhook Handler', () => {
  beforeEach(() => {
    jest.clearAllMocks();

    // Mock transaction to execute callback immediately
    mockPrismaClient.$transaction.mockImplementation(async (callback: any) => {
      return callback(mockPrismaClient);
    });
  });

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

      // Mock ZaloPay gateway
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

      // Mock PayOS gateway
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
    const mockPayment = {
      id: 'payment-123',
      orderCode: 'ORDER123',
      userId: 'user-456',
      amount: 100000,
      currency: 'VND',
      provider: PaymentProvider.ZALOPAY,
      status: PaymentStatus.PENDING,
      providerTransactionId: null,
      createdAt: new Date(),
      updatedAt: new Date(),
    };

    it('should successfully process valid ZaloPay callback', async () => {
      const callbackData = {
        app_trans_id: '250101_ORDER123',
        amount: 100000,
        app_user: 'user-456',
        app_time: Date.now(),
      };

      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: JSON.stringify({
          data: JSON.stringify(callbackData),
          mac: 'valid_mac',
          type: 1,
        }),
      });

      // Mock ZaloPay gateway success response
      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { return_code: 1, return_message: 'Success' },
        providerTransactionId: '12345678',
      });

      // Mock database calls
      mockPrismaClient.payment.findUnique.mockResolvedValue(mockPayment);
      mockPrismaClient.payment.update.mockResolvedValue({
        ...mockPayment,
        status: PaymentStatus.COMPLETED,
      });
      mockPrismaClient.outboxEvent.create.mockResolvedValue({
        id: 'outbox-123',
        aggregateId: mockPayment.id,
        aggregateType: 'payment',
        eventType: EventType.PAYMENT_COMPLETED,
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      const body = JSON.parse(result.body);
      expect(body.return_code).toBe(1);

      // Verify payment was updated
      expect(mockPrismaClient.payment.findUnique).toHaveBeenCalledWith({
        where: { orderCode: 'ORDER123' },
      });
      expect(mockPrismaClient.payment.update).toHaveBeenCalledWith({
        where: { id: mockPayment.id },
        data: expect.objectContaining({
          status: PaymentStatus.COMPLETED,
          providerTransactionId: '12345678',
        }),
      });

      // Verify outbox event was created
      expect(mockPrismaClient.outboxEvent.create).toHaveBeenCalledWith({
        data: expect.objectContaining({
          aggregateId: mockPayment.id,
          aggregateType: 'payment',
          eventType: EventType.PAYMENT_COMPLETED,
          payload: expect.objectContaining({
            type: EventType.PAYMENT_COMPLETED,
            data: expect.objectContaining({
              paymentId: mockPayment.id,
              orderCode: mockPayment.orderCode,
            }),
          }),
        }),
      });
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

      // Verify no database updates occurred
      expect(mockPrismaClient.payment.findUnique).not.toHaveBeenCalled();
      expect(mockPrismaClient.payment.update).not.toHaveBeenCalled();
    });

    it('should skip update if payment already completed', async () => {
      const completedPayment = {
        ...mockPayment,
        status: PaymentStatus.COMPLETED,
      };

      const callbackData = {
        app_trans_id: '250101_ORDER123',
        amount: 100000,
      };

      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: JSON.stringify({
          data: JSON.stringify(callbackData),
          mac: 'valid_mac',
          type: 1,
        }),
      });

      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { return_code: 1, return_message: 'Success' },
        providerTransactionId: '12345678',
      });

      mockPrismaClient.payment.findUnique.mockResolvedValue(completedPayment);

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);

      // Verify payment was found but not updated
      expect(mockPrismaClient.payment.findUnique).toHaveBeenCalled();
      expect(mockPrismaClient.payment.update).not.toHaveBeenCalled();
      expect(mockPrismaClient.outboxEvent.create).not.toHaveBeenCalled();
    });
  });

  describe('PayOS Webhook Processing', () => {
    const mockPayment = {
      id: 'payment-789',
      orderCode: 'ORDER456',
      userId: 'user-789',
      amount: 200000,
      currency: 'VND',
      provider: PaymentProvider.PAYOS,
      status: PaymentStatus.PENDING,
      providerTransactionId: 'payos-link-123',
      createdAt: new Date(),
      updatedAt: new Date(),
    };

    it('should successfully process valid PayOS callback', async () => {
      const webhookBody = {
        code: '00',
        desc: 'Success',
        data: {
          orderCode: 25010100000123,
          amount: 200000,
          paymentLinkId: 'payos-link-123',
        },
        signature: 'valid_signature',
      };

      const event = createMockAPIGatewayEvent({
        path: '/webhook/payos',
        body: JSON.stringify(webhookBody),
      });

      // Mock PayOS gateway success response
      const { PayOSGateway } = require('../gateways/payos/payos.gateway');
      PayOSGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { error: 0, message: 'Success' },
        providerTransactionId: 'payos-link-123',
      });

      // Mock database calls
      mockPrismaClient.payment.findFirst.mockResolvedValue(mockPayment);
      mockPrismaClient.payment.findUnique.mockResolvedValue(mockPayment);
      mockPrismaClient.payment.update.mockResolvedValue({
        ...mockPayment,
        status: PaymentStatus.COMPLETED,
      });
      mockPrismaClient.outboxEvent.create.mockResolvedValue({
        id: 'outbox-456',
        aggregateId: mockPayment.id,
        aggregateType: 'payment',
        eventType: EventType.PAYMENT_COMPLETED,
      });

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(200);
      const body = JSON.parse(result.body);
      expect(body.error).toBe(0);

      // Verify payment was looked up by provider transaction ID
      expect(mockPrismaClient.payment.findFirst).toHaveBeenCalledWith({
        where: { providerTransactionId: 'payos-link-123' },
      });

      // Verify payment was updated
      expect(mockPrismaClient.payment.update).toHaveBeenCalledWith({
        where: { id: mockPayment.id },
        data: expect.objectContaining({
          status: PaymentStatus.COMPLETED,
        }),
      });

      // Verify outbox event was created
      expect(mockPrismaClient.outboxEvent.create).toHaveBeenCalled();
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

      // Verify no database updates occurred
      expect(mockPrismaClient.payment.findUnique).not.toHaveBeenCalled();
      expect(mockPrismaClient.payment.update).not.toHaveBeenCalled();
    });

    it('should handle payment not found error', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/payos',
        body: JSON.stringify({
          code: '00',
          desc: 'Success',
          data: {
            orderCode: 123456,
            paymentLinkId: 'unknown-link',
          },
          signature: 'valid_signature',
        }),
      });

      const { PayOSGateway } = require('../gateways/payos/payos.gateway');
      PayOSGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { error: 0, message: 'Success' },
        providerTransactionId: 'unknown-link',
      });

      // Mock payment not found
      mockPrismaClient.payment.findFirst.mockResolvedValue(null);

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

    it('should handle database transaction errors', async () => {
      const event = createMockAPIGatewayEvent({
        path: '/webhook/zalopay',
        body: JSON.stringify({
          data: JSON.stringify({ app_trans_id: '250101_ORDER123' }),
          mac: 'valid_mac',
          type: 1,
        }),
      });

      const { ZalopayGateway } = require('../gateways/zalopay/zalopay.gateway');
      ZalopayGateway.prototype.handleCallback = jest.fn().mockResolvedValue({
        success: true,
        response: { return_code: 1, return_message: 'Success' },
        providerTransactionId: '12345678',
      });

      // Mock database error
      mockPrismaClient.$transaction.mockRejectedValue(new Error('Database connection failed'));

      const result = await handleWebhook(event);

      expect(result.statusCode).toBe(500);
      const body = JSON.parse(result.body);
      expect(body.error).toBe('Error');
      expect(body.message).toContain('Database connection failed');
    });
  });
});
