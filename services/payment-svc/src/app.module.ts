import { Module } from '@nestjs/common';
import { APP_FILTER, APP_INTERCEPTOR } from '@nestjs/core';
import { AppController } from './app.controller';
import { AppService } from './app.service';
import { GlobalGrpcExceptionFilter } from './common/filters/global-grpc-exception.filter';
import { GrpcMetricsInterceptor } from './common/interceptors/grpc-metrics.interceptor';
import { ResponseInterceptor } from './common/interceptors/response.interceptor';
import { TransformInterceptor } from './common/interceptors/transfrom.interceptor';
import { OutboxModule } from './modules/outbox/outbox.module';
import { PaymentModule } from './modules/payment/payment.module';
import { SharedModule } from './shared.module';

@Module({
  imports: [SharedModule, PaymentModule, OutboxModule.forRoot()],
  controllers: [AppController],
  providers: [
    AppService,
    // Registered first so it is the outermost global interceptor: an exception
    // raised by an inner one must still be counted.
    {
      provide: APP_INTERCEPTOR,
      useClass: GrpcMetricsInterceptor,
    },
    {
      provide: APP_FILTER,
      useClass: GlobalGrpcExceptionFilter,
    },
    {
      provide: APP_INTERCEPTOR,
      useClass: TransformInterceptor,
    },
    {
      provide: APP_INTERCEPTOR,
      useClass: ResponseInterceptor,
    },
  ],
})
export class AppModule {}
