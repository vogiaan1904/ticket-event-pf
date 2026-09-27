import { applyEdgeSecurity } from '@/common/security';
import { GlobalExceptionFilter } from '@/common/filters/global-exception.filter';
import { MetricsMiddleware } from '@middlewares/metrics.middleware';
import { grpcRequests } from '@/shared/metrics/metrics';
import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';
import {
  Controller,
  INestApplication,
  MiddlewareConsumer,
  Module,
  NestModule,
  Post,
  RequestMethod,
  UseGuards,
} from '@nestjs/common';
import { APP_FILTER } from '@nestjs/core';
import { NestExpressApplication } from '@nestjs/platform-express';
import { Test } from '@nestjs/testing';
import * as request from 'supertest';
import { AuthRateLimitGuard } from './auth-rate-limit.guard';

const LIMIT = 3;
let perMinute = LIMIT;

@Controller('auth')
class FakeAuthController {
  @Post('signin')
  @UseGuards(AuthRateLimitGuard)
  signin() {
    return { ok: true };
  }

  @Post('signup')
  @UseGuards(AuthRateLimitGuard)
  signup() {
    return { ok: true };
  }

  @Post('refresh')
  refresh() {
    return { ok: true };
  }
}

@Module({
  controllers: [FakeAuthController],
  providers: [
    { provide: APP_FILTER, useClass: GlobalExceptionFilter },
    {
      provide: AppConfigService,
      useFactory: () => ({ isDev: false, appConfig: { authRateLimitPerMinute: perMinute } }),
    },
    { provide: LoggerService, useValue: { setContext: jest.fn(), error: jest.fn() } },
  ],
})
class EdgeModule implements NestModule {
  configure(consumer: MiddlewareConsumer) {
    consumer.apply(MetricsMiddleware).forRoutes({ path: '*path', method: RequestMethod.ALL });
  }
}

async function boot(trustProxyHops: number): Promise<INestApplication> {
  const moduleRef = await Test.createTestingModule({ imports: [EdgeModule] }).compile();
  const app = moduleRef.createNestApplication<NestExpressApplication>();
  applyEdgeSecurity(app, { trustProxyHops, docs: false });
  await app.init();
  return app;
}

const throttledCount = async (method: string): Promise<number> => {
  const { values } = await grpcRequests.get();
  return (
    values.find((v) => v.labels.method === method && v.labels.code === 'RESOURCE_EXHAUSTED')
      ?.value ?? 0
  );
};

describe('AuthRateLimitGuard', () => {
  let app: INestApplication;

  beforeEach(() => {
    perMinute = LIMIT;
  });
  afterEach(async () => {
    await app?.close();
  });

  // A throttled sign-in is the gateway's own refusal, not a fault: labelled
  // INTERNAL it would page, and unlabelled it would read as success.
  it('answers 429 past the limit and records RESOURCE_EXHAUSTED under the route', async () => {
    app = await boot(0);
    const before = await throttledCount('POST /auth/signin');

    for (let i = 0; i < LIMIT; i++) {
      await request(app.getHttpServer()).post('/auth/signin').expect(201);
    }
    const res = await request(app.getHttpServer()).post('/auth/signin').expect(429);

    expect(res.body.success).toBe(false);
    expect(res.headers['ratelimit-policy']).toBeDefined();
    expect((await throttledCount('POST /auth/signin')) - before).toBe(1);
  });

  it('shares one budget between sign-in and sign-up', async () => {
    app = await boot(0);
    for (let i = 0; i < LIMIT; i++) {
      await request(app.getHttpServer()).post('/auth/signin').expect(201);
    }
    await request(app.getHttpServer()).post('/auth/signup').expect(429);
  });

  it('leaves routes without the guard alone', async () => {
    app = await boot(0);
    for (let i = 0; i < LIMIT + 2; i++) {
      await request(app.getHttpServer()).post('/auth/refresh').expect(201);
    }
  });

  // With no proxy in front (k3s NodePort), X-Forwarded-For is whatever the
  // client typed, so it must not buy a fresh budget.
  it('ignores X-Forwarded-For when no proxy hop is trusted', async () => {
    app = await boot(0);
    for (let i = 0; i < LIMIT; i++) {
      await request(app.getHttpServer())
        .post('/auth/signin')
        .set('X-Forwarded-For', `203.0.113.${i}`)
        .expect(201);
    }
    await request(app.getHttpServer())
      .post('/auth/signin')
      .set('X-Forwarded-For', '203.0.113.99')
      .expect(429);
  });

  // Behind the EKS ALB every request arrives from the balancer; keyed on the
  // socket, all buyers would share one budget.
  it('keys each client separately behind one trusted proxy', async () => {
    app = await boot(1);
    for (let i = 0; i < LIMIT; i++) {
      await request(app.getHttpServer())
        .post('/auth/signin')
        .set('X-Forwarded-For', '198.51.100.1')
        .expect(201);
    }
    await request(app.getHttpServer())
      .post('/auth/signin')
      .set('X-Forwarded-For', '198.51.100.1')
      .expect(429);
    await request(app.getHttpServer())
      .post('/auth/signin')
      .set('X-Forwarded-For', '198.51.100.2')
      .expect(201);
  });

  it('is off when the limit is 0', async () => {
    perMinute = 0;
    app = await boot(0);
    for (let i = 0; i < LIMIT + 2; i++) {
      await request(app.getHttpServer()).post('/auth/signin').expect(201);
    }
  });

  it('sets the security headers and drops X-Powered-By', async () => {
    app = await boot(0);
    const res = await request(app.getHttpServer()).post('/auth/refresh').expect(201);
    expect(res.headers['x-content-type-options']).toBe('nosniff');
    expect(res.headers['x-powered-by']).toBeUndefined();
  });
});
