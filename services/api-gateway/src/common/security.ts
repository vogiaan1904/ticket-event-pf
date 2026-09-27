import { NestExpressApplication } from '@nestjs/platform-express';
import helmet from 'helmet';

// trustProxyHops must equal the proxies in front: 1 behind the EKS ALB, 0 on
// the k3s NodePort, where X-Forwarded-For is client-typed and would let a
// client pick its own rate-limit key. CSP is off where Swagger UI is served.
export function applyEdgeSecurity(
  app: NestExpressApplication,
  opts: { trustProxyHops: number; docs: boolean },
): void {
  app.use(helmet({ contentSecurityPolicy: opts.docs ? false : undefined }));
  app.set('trust proxy', opts.trustProxyHops);
}
