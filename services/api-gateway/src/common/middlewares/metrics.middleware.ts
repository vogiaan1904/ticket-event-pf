import { SERVICE_NAME, grpcDuration, grpcInFlight, grpcRequests } from '@/shared/metrics/metrics';
import { TB_CODE } from '@/shared/metrics/code';
import { Injectable, NestMiddleware } from '@nestjs/common';
import { NextFunction, Request, Response } from 'express';

// Middleware, not an interceptor: Nest runs guards BEFORE interceptors, so an
// interceptor never sees an AccessGuard rejection and every 401 would be absent
// from the counter -- an auth outage would read as less traffic, not as errors.
@Injectable()
export class MetricsMiddleware implements NestMiddleware {
  use(req: Request, res: Response, next: NextFunction): void {
    const start = process.hrtime.bigint();
    grpcInFlight.inc({ service: SERVICE_NAME });

    let recorded = false;
    const record = (): void => {
      if (recorded) return;
      recorded = true;
      // The route pattern, never req.url: a path carrying an order code is one
      // series per buyer. The router fills req.route before either event.
      const method = `${req.method} ${req.route?.path ?? 'unmatched'}`;
      const code = (req as Request & { [TB_CODE]?: string })[TB_CODE] ?? 'OK';
      grpcRequests.inc({ service: SERVICE_NAME, method, code });
      grpcDuration.observe(
        { service: SERVICE_NAME, method },
        Number(process.hrtime.bigint() - start) / 1e9,
      );
      grpcInFlight.dec({ service: SERVICE_NAME });
    };

    // close as well as finish: an aborted connection emits only the former, and
    // an in-flight gauge that only counts up is worse than none.
    res.on('finish', record);
    res.on('close', record);

    next();
  }
}
