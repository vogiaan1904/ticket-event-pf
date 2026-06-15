import { Module } from '@nestjs/common';
import { EventsService } from './events.service';
import { EventsController } from './events.controller';
import { ClientsModule, Transport } from '@nestjs/microservices';
import { EVENT_PACKAGE_NAME, EVENT_SERVICE_NAME } from '@/protogen/event.pb';
import { AppConfigService } from '@/shared/services/config.service';
import { join } from 'path';

@Module({
  imports: [
    ClientsModule.registerAsync([
      {
        name: EVENT_SERVICE_NAME,
        useFactory: async (config: AppConfigService) => ({
          transport: Transport.GRPC,
          options: {
            url: config.microservicesConfig.eventsServiceUrl,
            package: EVENT_PACKAGE_NAME,
            protoPath: join(__dirname, '../../protos', 'event.proto'),
          },
        }),
        inject: [AppConfigService],
      },
    ]),
  ],
  controllers: [EventsController],
  providers: [EventsService],
})
export class EventsModule {}
