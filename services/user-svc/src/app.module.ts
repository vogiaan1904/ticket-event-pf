import { Module } from '@nestjs/common';
import { AppController } from './app.controller';
import { AppService } from './app.service';
import { UserModule } from './user/user.module';
import { PrismaModule } from './shared/prisma/prisma.module';
import { GlobalGrpcExceptionFilter } from './common/filters/global-grpc-exception.filter';
import { GrpcMetricsInterceptor } from './common/interceptors/grpc-metrics.interceptor';
import { APP_FILTER, APP_INTERCEPTOR } from '@nestjs/core';
import { SharedModule } from './shared.module';

@Module({
  imports: [SharedModule, UserModule, PrismaModule],
  controllers: [AppController],
  providers: [
    AppService,
    // Registered first so it is the outermost global interceptor: an exception
    // raised by an inner one must still be counted.
    { provide: APP_INTERCEPTOR, useClass: GrpcMetricsInterceptor },
    { provide: APP_FILTER, useClass: GlobalGrpcExceptionFilter },
  ],
})
export class AppModule {}
