import { status as grpcStatus } from '@grpc/grpc-js';
import { RpcException } from '@nestjs/microservices';
import { Test } from '@nestjs/testing';
import { PrismaService } from '@/shared/prisma/prisma.service';
import { CreateUserDto } from './dto/create-user.dto';
import { QueryUserDto } from './dto/query-user.dto';
import { UserServiceImpl } from './user.service';

const signup: CreateUserDto = {
  email: 'buyer@example.com',
  firstName: 'Buyer',
  lastName: 'One',
  password: '$argon2id$v=19$m=65536,t=3,p=4$hash',
};

async function buildService(prisma: any): Promise<UserServiceImpl> {
  const moduleRef = await Test.createTestingModule({
    providers: [UserServiceImpl, { provide: PrismaService, useValue: prisma }],
  }).compile();
  return moduleRef.get(UserServiceImpl);
}

// RpcException.getError() is typed `string | object`; every exception this
// service means to raise carries the object form. Anything else escaped
// unclassified, which is the defect these cases are about -- so report it as a
// missing code rather than crashing on the missing method.
const errorOf = (e: unknown): { code?: number; message?: string } =>
  e instanceof RpcException ? (e.getError() as { code?: number; message?: string }) : {};

const rejectionOf = (p: Promise<unknown>): Promise<unknown> =>
  p.then(
    () => null,
    (e) => e,
  );

describe('UserServiceImpl.create', () => {
  it('refuses an email the pre-check already sees', async () => {
    const service = await buildService({
      user: { findUnique: jest.fn().mockResolvedValue({ id: 'u-1' }), create: jest.fn() },
    });

    const error = await rejectionOf(service.create(signup));

    expect(errorOf(error).code).toBe(grpcStatus.ALREADY_EXISTS);
  });

  it('answers a lost signup race ALREADY_EXISTS, not a code that pages', async () => {
    const prisma = {
      user: {
        findUnique: jest.fn().mockResolvedValue(null),
        create: jest.fn().mockRejectedValue(Object.assign(new Error('unique'), { code: 'P2002' })),
      },
    };
    const service = await buildService(prisma);

    const error = await rejectionOf(service.create(signup));

    expect(errorOf(error).code).toBe(grpcStatus.ALREADY_EXISTS);
  });

  it('lets a failure that is not a duplicate through untouched', async () => {
    const service = await buildService({
      user: {
        findUnique: jest.fn().mockResolvedValue(null),
        create: jest.fn().mockRejectedValue(new Error('connection refused')),
      },
    });

    await expect(service.create(signup)).rejects.toThrow('connection refused');
  });
});

describe('UserServiceImpl.findAll', () => {
  const query = (over: Partial<QueryUserDto>): QueryUserDto => ({
    ids: undefined,
    emails: undefined,
    name: undefined,
    ...over,
  });

  it('selects nobody when nothing was asked for', async () => {
    const findMany = jest.fn();
    const service = await buildService({ user: { findMany } });

    expect(await service.findAll(query({}))).toEqual([]);
    expect(findMany).not.toHaveBeenCalled();
  });

  it('builds a clause only for the filters it was given', async () => {
    const findMany = jest.fn().mockResolvedValue([]);
    const service = await buildService({ user: { findMany } });

    await service.findAll(query({ ids: ['u-1'], emails: [] }));

    expect(findMany).toHaveBeenCalledWith({ where: { OR: [{ id: { in: ['u-1'] } }] } });
  });

  it('searches both names for one term', async () => {
    const findMany = jest.fn().mockResolvedValue([]);
    const service = await buildService({ user: { findMany } });

    await service.findAll(query({ name: 'Buyer' }));

    expect(findMany).toHaveBeenCalledWith({
      where: {
        OR: [{ firstName: { contains: 'Buyer' } }, { lastName: { contains: 'Buyer' } }],
      },
    });
  });

  it('lets a query failure reach the caller instead of returning undefined', async () => {
    const service = await buildService({
      user: { findMany: jest.fn().mockRejectedValue(new Error('db down')) },
    });

    await expect(service.findAll(query({ name: 'Buyer' }))).rejects.toThrow('db down');
  });
});
