import { ValidationPipe } from '@nestjs/common';
import { NestFactory } from '@nestjs/core';
import { MicroserviceOptions, Transport } from '@nestjs/microservices';
import { LoggerService } from '@shared/services/logger.service';
import { AppModule } from './app.module';
import { RpcValidationException } from './common/exceptions/rpc-validation.exception';
import { PAYMENT_PACKAGE_NAME } from './protogen/payment.pb';
import { join } from 'path/win32';

async function bootstrap() {
  const HOST = '0.0.0.0';
  const GRPC_PORT = process.env.GRPC_PORT || '50055';

  const app = await NestFactory.createMicroservice<MicroserviceOptions>(AppModule, {
    transport: Transport.GRPC,
    options: {
      url: `${HOST}:${GRPC_PORT}`,
      package: PAYMENT_PACKAGE_NAME,
      protoPath: join(__dirname, 'protos', 'payment.proto'),
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
  logger.log(`gRPC Server running on: ${HOST}:${GRPC_PORT}`);
}

bootstrap();
