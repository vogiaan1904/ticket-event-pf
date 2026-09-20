import { status as GrpcStatus } from '@grpc/grpc-js';
import { ArgumentsHost, HttpStatus } from '@nestjs/common';
import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';
import { GlobalExceptionFilter } from './global-exception.filter';

function buildFilter() {
  const json = jest.fn();
  const response = { status: jest.fn().mockReturnThis(), json };
  const request: Record<string | symbol, unknown> = {};
  const host = {
    switchToHttp: () => ({ getResponse: () => response, getRequest: () => request }),
  } as unknown as ArgumentsHost;

  const config = { isDev: false } as AppConfigService;
  const logger = { setContext: jest.fn(), error: jest.fn() } as unknown as LoggerService;

  return { filter: new GlobalExceptionFilter(config, logger), host, response, json, request };
}

// The taxonomy in the root CLAUDE.md, which this filter is the only
// implementation of. A change here is a change to the platform's contract.
const TAXONOMY: ReadonlyArray<[GrpcStatus, HttpStatus]> = [
  [GrpcStatus.INVALID_ARGUMENT, HttpStatus.BAD_REQUEST],
  [GrpcStatus.UNAUTHENTICATED, HttpStatus.UNAUTHORIZED],
  [GrpcStatus.PERMISSION_DENIED, HttpStatus.FORBIDDEN],
  [GrpcStatus.NOT_FOUND, HttpStatus.NOT_FOUND],
  [GrpcStatus.ALREADY_EXISTS, HttpStatus.CONFLICT],
  [GrpcStatus.FAILED_PRECONDITION, HttpStatus.CONFLICT],
  [GrpcStatus.UNAVAILABLE, HttpStatus.SERVICE_UNAVAILABLE],
  [GrpcStatus.DEADLINE_EXCEEDED, HttpStatus.GATEWAY_TIMEOUT],
];

describe('GlobalExceptionFilter', () => {
  it.each(TAXONOMY)('maps gRPC %i to HTTP %i', (grpcCode, httpStatus) => {
    const { filter, host, response } = buildFilter();

    filter.catch({ code: grpcCode, message: 'downstream said no' }, host);

    expect(response.status).toHaveBeenCalledWith(httpStatus);
  });

  it('answers 500 for INTERNAL, which is the one code that means our bug', () => {
    const { filter, host, response } = buildFilter();

    filter.catch({ code: GrpcStatus.INTERNAL, message: 'boom' }, host);

    expect(response.status).toHaveBeenCalledWith(HttpStatus.INTERNAL_SERVER_ERROR);
  });
});
