import { INestMicroservice, ValidationPipe } from '@nestjs/common';
import { NestFactory } from '@nestjs/core';
import { Transport } from '@nestjs/microservices';
import { LoggerService } from '@shared/services/logger.service';
import { AppModule } from './app.module';
import { RpcValidationException } from './common/exceptions/rpc-validation.exception';
import { EVENT_PACKAGE_NAME } from './protogen/event.pb';
import { join } from 'path/win32';
import { startMetricsServer } from './shared/metrics/server';

async function bootstrap() {
  const HOST = process.env.HOST || '0.0.0.0';
  const PORT = process.env.GRPC_PORT || '50053';
  const METRICS_PORT = Number(process.env.SERVER_METRICS_PORT || 2112);

  const app: INestMicroservice = await NestFactory.createMicroservice(AppModule, {
    transport: Transport.GRPC,
    options: {
      url: `${HOST}:${PORT}`,
      package: EVENT_PACKAGE_NAME,
      protoPath: join(__dirname, 'protos', 'event.proto'),
    },
  });

  const logger = app.get(LoggerService);
  app.useLogger(logger);

  app.useGlobalPipes(
    new ValidationPipe({
      whitelist: true,
      transform: true,
      exceptionFactory: (errors) => {
        throw new RpcValidationException('Validation failed', errors);
      },
    }),
  );

  await app.listen();
  logger.log(`gRPC Server running on: ${HOST}:${PORT}`);

  const metricsServer = startMetricsServer(METRICS_PORT);
  logger.log(`metrics server running on: ${HOST}:${METRICS_PORT}/metrics`);

  for (const sig of ['SIGTERM', 'SIGINT'] as const) {
    process.on(sig, () => metricsServer.close());
  }
}

bootstrap();
