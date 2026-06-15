import { Module } from '@nestjs/common';
import { APP_FILTER, APP_INTERCEPTOR } from '@nestjs/core';
import { AppController } from './app.controller';
import { AppService } from './app.service';
import { GlobalGrpcExceptionFilter } from './common/filters/global-grpc-exception.filter';
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
