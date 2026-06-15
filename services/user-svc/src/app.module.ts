import { Module } from '@nestjs/common';
import { AppController } from './app.controller';
import { AppService } from './app.service';
import { UserModule } from './user/user.module';
import { PrismaModule } from './shared/prisma/prisma.module';
import { GlobalGrpcExceptionFilter } from './common/filters/global-grpc-exception.filter';
import { APP_FILTER } from '@nestjs/core';
import { SharedModule } from './shared.module';

@Module({
  imports: [SharedModule, UserModule, PrismaModule],
  controllers: [AppController],
  providers: [AppService, { provide: APP_FILTER, useClass: GlobalGrpcExceptionFilter }],
})
export class AppModule {}
