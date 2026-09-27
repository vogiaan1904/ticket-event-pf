import * as http from 'http';
import { AddressInfo } from 'net';
import { createHttpDrain } from './http-drain.util';

type Handler = (req: http.IncomingMessage, res: http.ServerResponse) => void;

// serve runs handler behind a drain, with a keep-alive client of up to four sockets.
async function serve(handler: Handler) {
  const drain = createHttpDrain();
  const server = http.createServer((req, res) =>
    drain.middleware(req, res, () => handler(req, res)),
  );
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  // Read once: server.address() is null after close().
  const { port } = server.address() as AddressInfo;
  const agent = new http.Agent({ keepAlive: true, maxSockets: 4 });
  const get = () =>
    new Promise<http.IncomingMessage>((resolve, reject) => {
      http
        .get({ host: '127.0.0.1', port, agent }, (res) => {
          res.resume();
          res.on('end', () => resolve(res));
        })
        .on('error', reject);
    });
  const stop = (deadlineMs: number) => {
    const started = Date.now();
    drain.begin(server, deadlineMs);
    return new Promise<number>((resolve) => server.close(() => resolve(Date.now() - started)));
  };
  return { get, stop, agent };
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe('createHttpDrain', () => {
  it('tells keep-alive clients with a request in flight to reconnect, then closes', async () => {
    let holding = true;
    const held: Array<() => void> = [];
    const { get, stop, agent } = await serve((_req, res) => {
      if (holding) held.push(() => res.end('ok'));
      else res.end('ok');
    });
    let toldToLeave = 0;
    const client = async () => {
      for (;;) {
        const res = await get();
        if (res.headers.connection === 'close') {
          toldToLeave++;
          return;
        }
      }
    };
    const clients = [client(), client(), client(), client()];
    while (held.length < 4) await sleep(5);

    const stopped = stop(10_000);
    holding = false;
    held.forEach((respond) => respond());
    await Promise.all(clients);

    expect(toldToLeave).toBe(4);
    expect(await stopped).toBeLessThan(1_000);
    agent.destroy();
  });

  it('lets a request in flight finish', async () => {
    const { get, stop, agent } = await serve((_req, res) => setTimeout(() => res.end('ok'), 300));
    const pending = get();
    await sleep(50);

    const stopped = stop(10_000);
    const res = await pending;
    agent.destroy();

    expect(res.statusCode).toBe(200);
    await stopped;
  });

  it('cuts a request still running at the deadline', async () => {
    const { get, stop, agent } = await serve(() => {});
    const pending = get().then(
      () => 'answered',
      (err: NodeJS.ErrnoException) => err.code,
    );
    await sleep(50);

    const ms = await stop(200);

    expect(await pending).toBe('ECONNRESET');
    expect(ms).toBeLessThan(1_000);
    agent.destroy();
  });
});
