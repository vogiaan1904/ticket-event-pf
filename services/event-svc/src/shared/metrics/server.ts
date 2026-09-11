import { Server, createServer } from 'http';
import { register } from 'prom-client';

// startMetricsServer starts the :port/metrics listener. It mirrors how the gRPC
// server is built in main.ts -- started here, closed by the caller, not
// self-managed.
export const startMetricsServer = (port: number): Server =>
  createServer((req, res) => {
    if (req.url !== '/metrics') {
      res.writeHead(404).end();
      return;
    }
    register
      .metrics()
      .then((body) => res.writeHead(200, { 'Content-Type': register.contentType }).end(body))
      .catch(() => res.writeHead(500).end());
  }).listen(port);
