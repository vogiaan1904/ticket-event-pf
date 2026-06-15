import { Module } from '@nestjs/common';
import { OrdersService } from './orders.service';
import { OrdersController } from './orders.controller';
import { ClientsModule, Transport } from '@nestjs/microservices';
import { ORDER_PACKAGE_NAME, ORDER_SERVICE_NAME } from '@/protogen/order.pb';
import { AppConfigService } from '@/shared/services/config.service';
import { join } from 'path';

@Module({
  imports: [
    ClientsModule.registerAsync([
      {
        name: ORDER_SERVICE_NAME,
        useFactory: async (config: AppConfigService) => ({
          transport: Transport.GRPC,
          options: {
            url: config.microservicesConfig.orderServiceUrl,
            package: ORDER_PACKAGE_NAME,
            protoPath: join(__dirname, '../../protos', 'order.proto'),
          },
        }),
        inject: [AppConfigService],
      },
    ]),
  ],
  controllers: [OrdersController],
  providers: [OrdersService],
})
export class OrdersModule {}
