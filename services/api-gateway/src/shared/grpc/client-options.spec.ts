import * as grpc from '@grpc/grpc-js';
import { ClientGrpcProxy } from '@nestjs/microservices';
import { join } from 'path';
import { firstValueFrom } from 'rxjs';
import { EVENT_PACKAGE_NAME, EVENT_SERVICE_NAME, EventServiceClient } from '@/protogen/event.pb';
import { GRPC_LOADER_OPTIONS } from '@/shared/constants/grpc.constant';
import { BoundedChannel } from './bounded-channel';
import { CALL_DEADLINE_MS, clientChannelOptions, deadlineInterceptor } from './client-options';

const id = (b: Buffer) => b;
const ECHO = {
  Echo: {
    path: '/t.T/Echo',
    requestStream: false,
    responseStream: false,
    requestSerialize: id,
    requestDeserialize: id,
    responseSerialize: id,
    responseDeserialize: id,
  },
};

describe('deadlineInterceptor', () => {
  const server = new grpc.Server();
  let port = 0;
  beforeAll(async () => {
    server.addService(ECHO, {
      Echo: (_c: unknown, cb: grpc.sendUnaryData<Buffer>) =>
        setTimeout(() => cb(null, Buffer.from('ok')), 300),
    });
    port = await new Promise<number>((res, rej) =>
      server.bindAsync('127.0.0.1:0', grpc.ServerCredentials.createInsecure(), (e, p) =>
        e ? rej(e) : res(p),
      ),
    );
  });
  afterAll(() => server.forceShutdown());

  // Resolves with the call's status code and how long it took.
  const call = (interceptor: grpc.Interceptor, deadline?: number) => {
    const Client = grpc.makeGenericClientConstructor(ECHO, 'T');
    const client = new Client(`127.0.0.1:${port}`, grpc.credentials.createInsecure(), {
      interceptors: [interceptor],
    }) as any;
    const start = Date.now();
    return new Promise<{ code: grpc.status; ms: number }>((res) =>
      client.Echo(Buffer.from('x'), deadline ? { deadline } : {}, (e: grpc.ServiceError | null) => {
        client.close();
        res({ code: e ? e.code : grpc.status.OK, ms: Date.now() - start });
      }),
    );
  };

  it('ends a call with no deadline of its own at the bound', async () => {
    const { code, ms } = await call(deadlineInterceptor(100));
    expect(code).toBe(grpc.status.DEADLINE_EXCEEDED);
    expect(ms).toBeLessThan(250);
  });

  it('gives a method with a longer bound its own', async () => {
    const { code } = await call(deadlineInterceptor(100, { '/t.T/Echo': 1000 }));
    expect(code).toBe(grpc.status.OK);
  });

  it('keeps a deadline the caller set', async () => {
    const { code, ms } = await call(deadlineInterceptor(1000), Date.now() + 100);
    expect(code).toBe(grpc.status.DEADLINE_EXCEEDED);
    expect(ms).toBeLessThan(250);
  });
});

describe('the gateway’s bounds', () => {
  // Returns the deadline the default interceptor hands the call for a method path.
  const boundFor = (path: string) => {
    let deadline = 0;
    const next = (o: grpc.InterceptorOptions) => {
      deadline = o.deadline as number;
      return {} as never;
    };
    const now = Date.now();
    deadlineInterceptor()({ method_definition: { path } } as grpc.InterceptorOptions, next);
    return deadline - now;
  };

  it('gives CreateOrder 65s, above order-svc’s own 60s', () => {
    expect(boundFor('/order.OrderService/CreateOrder')).toBeGreaterThanOrEqual(65_000);
    expect(boundFor('/order.OrderService/CreateOrder')).toBeLessThan(65_100);
  });

  it('gives every other call 5s', () => {
    expect(boundFor('/event.EventService/FindOne')).toBeLessThan(CALL_DEADLINE_MS + 100);
  });
});

describe('clientChannelOptions', () => {
  it('bounds a Nest client’s call to a backend that never answers it', async () => {
    const silent = new grpc.Server();
    silent.addService(
      { FindOne: { ...ECHO.Echo, path: `/${EVENT_PACKAGE_NAME}.${EVENT_SERVICE_NAME}/FindOne` } },
      { FindOne: () => undefined },
    );
    const port = await new Promise<number>((res, rej) =>
      silent.bindAsync('127.0.0.1:0', grpc.ServerCredentials.createInsecure(), (e, p) =>
        e ? rej(e) : res(p),
      ),
    );
    const proxy = new ClientGrpcProxy({
      url: `127.0.0.1:${port}`,
      package: EVENT_PACKAGE_NAME,
      protoPath: join(__dirname, '../../protos', 'event.proto'),
      loader: GRPC_LOADER_OPTIONS,
      channelOptions: clientChannelOptions(),
    });
    const client = proxy.getService<EventServiceClient>(EVENT_SERVICE_NAME);
    expect(grpc.getClientChannel(proxy.getClientByServiceName(EVENT_SERVICE_NAME))).toBeInstanceOf(
      BoundedChannel,
    );

    const start = Date.now();
    const err = await firstValueFrom(client.findOne({ id: 'x' })).catch((e) => e);
    const ms = Date.now() - start;
    proxy.close();
    silent.forceShutdown();

    expect(err.code).toBe(grpc.status.DEADLINE_EXCEEDED);
    expect(ms).toBeGreaterThanOrEqual(CALL_DEADLINE_MS - 50);
    expect(ms).toBeLessThan(CALL_DEADLINE_MS + 1000);
  }, 15_000);
});
