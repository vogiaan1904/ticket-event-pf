import * as grpc from '@grpc/grpc-js';
import { Logger } from '@nestjs/common';
import * as net from 'net';
import { ClientGrpcProxy } from '@nestjs/microservices';
import { join } from 'path';
import { EVENT_PACKAGE_NAME, EVENT_SERVICE_NAME } from '@/protogen/event.pb';
import { GRPC_LOADER_OPTIONS } from '@/shared/constants/grpc.constant';
import { BoundedChannel, boundedChannelFactory } from './bounded-channel';

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
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

// A backend behind a proxy that can be cut, refused, then come back with its
// first accepted connection never answered: the hang grpc-js does not bound.
class FlakyBackend {
  private server = new grpc.Server();
  private listener?: net.Server;
  private socks = new Set<net.Socket>();
  private upstream = 0;
  private stallNext = false;
  port = 0;

  async start(): Promise<number> {
    this.server.addService(ECHO, {
      Echo: (_c: unknown, cb: grpc.sendUnaryData<Buffer>) => cb(null, Buffer.from('ok')),
    });
    this.upstream = await new Promise<number>((res, rej) =>
      this.server.bindAsync('127.0.0.1:0', grpc.ServerCredentials.createInsecure(), (e, p) =>
        e ? rej(e) : res(p),
      ),
    );
    this.port = await this.listen(0);
    return this.port;
  }

  private listen(port: number): Promise<number> {
    this.listener = net.createServer((c) => {
      this.socks.add(c);
      c.on('close', () => this.socks.delete(c)).on('error', () => undefined);
      if (this.stallNext) return void (this.stallNext = false);
      const up = net.connect(this.upstream, '127.0.0.1').on('error', () => c.destroy());
      c.on('close', () => up.destroy());
      c.pipe(up).pipe(c);
    });
    return new Promise((res) =>
      this.listener!.listen(port, '127.0.0.1', () =>
        res((this.listener!.address() as net.AddressInfo).port),
      ),
    );
  }

  async crash(refuseMs: number) {
    this.listener!.close();
    this.socks.forEach((s) => s.destroy());
    await sleep(refuseMs);
    this.stallNext = true;
    await this.listen(this.port);
  }

  stop() {
    this.listener?.close();
    this.socks.forEach((s) => s.destroy());
    this.server.forceShutdown();
  }
}

describe('boundedChannelFactory', () => {
  let backend: FlakyBackend;
  afterEach(() => backend.stop());

  it('recovers when the first reconnect after a crash is accepted and never answered', async () => {
    backend = new FlakyBackend();
    const port = await backend.start();
    const Client = grpc.makeGenericClientConstructor(ECHO, 'T');
    const client = new Client(`127.0.0.1:${port}`, grpc.credentials.createInsecure(), {
      channelFactoryOverride: boundedChannelFactory(1000),
    }) as any;
    const echo = () =>
      new Promise<boolean>((res) =>
        client.Echo(Buffer.from('x'), { deadline: Date.now() + 500 }, (e: unknown) => res(!e)),
      );

    expect(await echo()).toBe(true);
    const crashed = Date.now();
    const crash = backend.crash(1800);

    let recoveredAt = 0;
    while (!recoveredAt && Date.now() - crashed < 10_000) {
      if ((await echo()) && Date.now() - crashed > 1800) recoveredAt = Date.now();
      else await sleep(50);
    }
    await crash;
    client.close();
    expect(recoveredAt).toBeGreaterThan(0);
    expect(recoveredAt - crashed).toBeLessThan(6000);
  }, 15_000);

  it('never replaces a channel that connects, serves, then sits idle', async () => {
    backend = new FlakyBackend();
    const port = await backend.start();
    const warn = jest.spyOn(Logger.prototype, 'warn').mockImplementation(() => undefined);
    const Client = grpc.makeGenericClientConstructor(ECHO, 'T');
    const client = new Client(`127.0.0.1:${port}`, grpc.credentials.createInsecure(), {
      channelFactoryOverride: boundedChannelFactory(200),
    }) as any;

    for (let i = 0; i < 20; i++) {
      await new Promise((res) => client.Echo(Buffer.from('x'), res));
      await sleep(25);
    }
    await sleep(600);
    client.close();
    expect(warn).not.toHaveBeenCalled();
    warn.mockRestore();
  });

  it('reaches the grpc-js client through Nest’s ClientsModule options', () => {
    const proxy = new ClientGrpcProxy({
      url: '127.0.0.1:1',
      package: EVENT_PACKAGE_NAME,
      protoPath: join(__dirname, '../../protos', 'event.proto'),
      loader: GRPC_LOADER_OPTIONS,
      channelOptions: { channelFactoryOverride: boundedChannelFactory() },
    });
    const client = proxy.getClientByServiceName<grpc.Client>(EVENT_SERVICE_NAME);
    expect(grpc.getClientChannel(client)).toBeInstanceOf(BoundedChannel);
    proxy.close();
  });
});
