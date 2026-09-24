import { AppConfigService } from '@services/config.service';
import {
  CanActivate,
  ExecutionContext,
  HttpException,
  HttpStatus,
  Injectable,
} from '@nestjs/common';
import { Request, Response } from 'express';
import { RateLimitRequestHandler, rateLimit } from 'express-rate-limit';

// AuthRateLimitGuard caps sign-in and sign-up attempts per client IP.
// Why: each one runs an argon2 hash (64 MiB, on libuv's 4 threads) in this
// process, so an unthrottled flood degrades every route the gateway serves.
// Trade-off: the count is per replica, so N replicas admit N times the limit.
@Injectable()
export class AuthRateLimitGuard implements CanActivate {
  private readonly limiter: RateLimitRequestHandler | null;

  constructor(config: AppConfigService) {
    const perMinute = config.appConfig.authRateLimitPerMinute;
    if (!Number.isInteger(perMinute) || perMinute < 0) {
      throw new Error(`APP_AUTH_RATE_LIMIT_PER_MINUTE must be a whole number, got ${perMinute}`);
    }
    this.limiter =
      perMinute === 0
        ? null
        : rateLimit({
            windowMs: 60_000,
            limit: perMinute,
            standardHeaders: 'draft-8',
            legacyHeaders: false,
            // Through the exception filter, so the 429 is labelled like any other refusal.
            handler: (_req, _res, next) =>
              next(new HttpException('Too many attempts; retry later', HttpStatus.TOO_MANY_REQUESTS)),
          });
  }

  canActivate(ctx: ExecutionContext): boolean | Promise<boolean> {
    const limiter = this.limiter;
    if (!limiter) return true;
    const http = ctx.switchToHttp();
    return new Promise((resolve, reject) => {
      void limiter(http.getRequest<Request>(), http.getResponse<Response>(), (err?: unknown) =>
        err ? reject(err) : resolve(true),
      );
    });
  }
}
