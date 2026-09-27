import type { IncomingMessage, Server, ServerResponse } from 'http';

// HttpDrain lets a stopping HTTP server shed keep-alive clients instead of waiting on them.
// See docs/decisions/0016-the-gateway-drains-for-up-to-65s.md.
export interface HttpDrain {
  middleware(req: IncomingMessage, res: ServerResponse, next: () => void): void;
  begin(server: Server, deadlineMs: number): void;
}

// createHttpDrain returns a drain whose middleware must run on every request.
export function createHttpDrain(): HttpDrain {
  let draining = false;
  return {
    middleware(_req, res, next) {
      // Why: server.close() waits on every open socket, and a busy keep-alive
      // client never lets its socket go idle. The header makes it reconnect.
      if (draining) res.setHeader('Connection', 'close');
      next();
    },
    begin(server, deadlineMs) {
      draining = true;
      server.closeIdleConnections();
      setTimeout(() => server.closeAllConnections(), deadlineMs).unref();
    },
  };
}
