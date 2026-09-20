import { status as GrpcStatus } from '@grpc/grpc-js';
import { BadRequestException, InternalServerErrorException } from '@nestjs/common';
import { grpcCodeOf } from './code';

describe('grpcCodeOf', () => {
  it('labels a downstream code the filter maps with that code', () => {
    expect(grpcCodeOf({ code: GrpcStatus.NOT_FOUND })).toBe('NOT_FOUND');
  });

  // The filter answers 500 for any code absent from GRPC_TO_HTTP. Labelling
  // that request anything but INTERNAL hides it from the only alert that pages.
  it.each([
    GrpcStatus.RESOURCE_EXHAUSTED,
    GrpcStatus.CANCELLED,
    GrpcStatus.UNKNOWN,
    GrpcStatus.DATA_LOSS,
    GrpcStatus.OK,
  ])('labels an unmapped downstream code %i as INTERNAL', (code) => {
    expect(grpcCodeOf({ code })).toBe('INTERNAL');
  });

  it('labels a gateway HttpException with the code its status came from', () => {
    expect(grpcCodeOf(new BadRequestException())).toBe('INVALID_ARGUMENT');
  });

  it('labels a gateway 500 as INTERNAL', () => {
    expect(grpcCodeOf(new InternalServerErrorException())).toBe('INTERNAL');
  });

  it('labels anything else INTERNAL', () => {
    expect(grpcCodeOf(new Error('boom'))).toBe('INTERNAL');
  });
});
