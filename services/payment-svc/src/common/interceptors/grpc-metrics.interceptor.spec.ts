import { status as grpcStatus } from '@grpc/grpc-js';
import { CallHandler, ExecutionContext } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { Observable, of, throwError } from 'rxjs';
import { grpcRequests } from '@/shared/metrics/metrics';
import { GrpcMetricsInterceptor } from './grpc-metrics.interceptor';

const context = {
  getArgByIndex: () => ({ getPath: () => '/payment.PaymentService/CreatePaymentIntent' }),
  getClass: () => ({ name: 'PaymentController' }),
  getHandler: () => ({ name: 'createPaymentIntent' }),
} as unknown as ExecutionContext;

const handlerOf = (result: Observable<unknown>): CallHandler => ({ handle: () => result });

// The label under which this call was counted, which is the only thing an
// alert rule can see.
async function codeLabelAfter(result: Observable<unknown>): Promise<string> {
  const interceptor = new GrpcMetricsInterceptor();
  await new Promise<void>((resolve) => {
    interceptor.intercept(context, handlerOf(result)).subscribe({
      next: () => undefined,
      error: () => resolve(),
      complete: () => resolve(),
    });
  });

  const counted = await grpcRequests.get();
  return String(counted.values[0].labels.code);
}

describe('GrpcMetricsInterceptor', () => {
  beforeEach(() => grpcRequests.reset());

  it('counts a success as OK', async () => {
    expect(await codeLabelAfter(of({ paymentUrl: 'https://pay/1' }))).toBe('OK');
  });

  it('counts a business rejection under its own code', async () => {
    const rejection = new RpcException({
      code: grpcStatus.NOT_FOUND,
      message: '20000 - Payment not found',
    });

    expect(await codeLabelAfter(throwError(() => rejection))).toBe('NOT_FOUND');
  });

  it('counts an unclassified failure as INTERNAL, the code the alert watches', async () => {
    expect(await codeLabelAfter(throwError(() => new Error('prisma exploded')))).toBe('INTERNAL');
  });
});
