import { INestMicroservice, ValidationPipe } from '@nestjs/common';
import { NestFactory } from '@nestjs/core';
import { Transport } from '@nestjs/microservices';
import { join } from 'path';
import { AppModule } from './app.module';
import { RpcValidationException } from './common/exceptions/rpc-validation.exception';
import { GlobalGrpcExceptionFilter } from './common/filters/global-grpc-exception.filter';
import { USER_PACKAGE_NAME } from './protogen/user.pb';
import { LoggerService } from './shared/services/logger.service';

async function bootstrap() {
  const HOST = process.env.HOST || '0.0.0.0';
  const PORT = process.env.PORT || 50052;
  const app: INestMicroservice = await NestFactory.createMicroservice(AppModule, {
    transport: Transport.GRPC,
    options: {
      url: `${HOST}:${PORT}`,
      package: USER_PACKAGE_NAME,
      protoPath: join(__dirname, 'protos', 'user.proto'),
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
  app.useGlobalFilters(new GlobalGrpcExceptionFilter(logger));
  await app.listen();
}
bootstrap();
