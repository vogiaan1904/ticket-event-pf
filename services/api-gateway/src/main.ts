import { ValidationPipe } from '@nestjs/common';
import { NestFactory } from '@nestjs/core';
import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';
import { AppModule } from './app.module';
import { startMetricsServer } from './shared/metrics/server';
import { setupSwagger } from './shared/swagger/setup';

async function bootstrap() {
  const app = await NestFactory.create(AppModule);

  const configService = app.get(AppConfigService);
  const logger = app.get(LoggerService);
  const isDocsEnv = ['development', 'staging'].includes(configService.nodeEnv);

  app.useLogger(logger);

  app.setGlobalPrefix(configService.appConfig.globalPrefix || 'api');
  app.useGlobalPipes(new ValidationPipe({ transform: true }));

  const corsOriginsRaw = configService.appConfig.corsOrigins;
  const corsOrigins = Array.isArray(corsOriginsRaw)
    ? (corsOriginsRaw as unknown as string[])
    : typeof corsOriginsRaw === 'string'
      ? corsOriginsRaw
          .split(',')
          .map((o) => o.trim())
          .filter(Boolean)
      : [];
  const effectiveCorsOrigins = corsOrigins.length > 0 ? corsOrigins : ['http://localhost:3000'];
  app.enableCors({ origin: effectiveCorsOrigins, credentials: true });

  const port = configService.appConfig.port || 3000;
  const metricsPort = Number(process.env.SERVER_METRICS_PORT || 2112);

  if (isDocsEnv) {
    setupSwagger(app, configService.swaggerConfig);
  }

  await app.listen(port);

  const metricsServer = startMetricsServer(metricsPort);
  logger.log(`metrics server running on: http://localhost:${metricsPort}/metrics`);

  for (const sig of ['SIGTERM', 'SIGINT'] as const) {
    process.on(sig, () => metricsServer.close());
  }

  logger.log(
    `Application is running on: http://localhost:${port}/${configService.appConfig.globalPrefix || 'api'}`,
  );

  if (isDocsEnv) {
    logger.info(
      `See the API docs on: http://localhost:${port}/${configService.appConfig.globalPrefix || 'api'}/${configService.swaggerConfig.path || 'docs'}`,
    );
  }
}

bootstrap();
