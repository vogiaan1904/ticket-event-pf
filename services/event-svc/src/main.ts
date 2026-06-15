import { INestMicroservice, ValidationPipe } from '@nestjs/common';
import { NestFactory } from '@nestjs/core';
import { Transport } from '@nestjs/microservices';
import { LoggerService } from '@shared/services/logger.service';
import { AppModule } from './app.module';
import { RpcValidationException } from './common/exceptions/rpc-validation.exception';
import { EVENT_PACKAGE_NAME } from './protogen/event.pb';
import { join } from 'path/win32';

async function bootstrap() {
  const HOST = process.env.HOST || '0.0.0.0';
  const PORT = process.env.GRPC_PORT || '50053';

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
}

bootstrap();
