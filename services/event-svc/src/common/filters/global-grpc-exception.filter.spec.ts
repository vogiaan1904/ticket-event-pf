import { status as grpcStatus } from '@grpc/grpc-js';
import { ArgumentsHost } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { LoggerService } from '@/shared/services/logger.service';
import { GlobalGrpcExceptionFilter } from './global-grpc-exception.filter';

const host = {} as ArgumentsHost;

const loggerStub = {
  setContext: jest.fn(),
  error: jest.fn(),
} as unknown as LoggerService;

// What the filter puts on the wire, which is what grpc-js turns into a status.
async function wireError(exception: unknown): Promise<{ code?: number; message?: string }> {
  const filter = new GlobalGrpcExceptionFilter(loggerStub);
  return new Promise((resolve) => {
    filter.catch(exception, host).subscribe({
      error: (e) => resolve(e as { code?: number; message?: string }),
    });
  });
}

describe('GlobalGrpcExceptionFilter', () => {
  it('passes a business rejection through with its own code', async () => {
    const sent = await wireError(
      new RpcException({ code: grpcStatus.NOT_FOUND, message: '20000 - Event not found' }),
    );

    expect(sent.code).toBe(grpcStatus.NOT_FOUND);
    expect(sent.message).toBe('20000 - Event not found');
  });

  it('sends an unexpected failure as INTERNAL, not as a codeless error', async () => {
    const sent = await wireError(new Error('prisma exploded'));

    expect(sent.code).toBe(grpcStatus.INTERNAL);
    expect(sent.message).toBe('Internal server error');
  });

  it('does not leak the underlying message to the caller', async () => {
    const sent = await wireError(new Error('connect ECONNREFUSED 10.0.1.4:5432'));

    expect(sent.message).not.toContain('10.0.1.4');
  });
});
