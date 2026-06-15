import { Module } from '@nestjs/common';
import { WaitroomService } from './waitroom.service';
import { WaitroomController } from './waitroom.controller';
import { ClientsModule, Transport } from '@nestjs/microservices';
import { WAITROOM_SERVICE_NAME, WAITROOM_V1_PACKAGE_NAME } from '@/protogen/waitroom.pb';
import { AppConfigService } from '@/shared/services/config.service';
import { join } from 'path';

@Module({
  imports: [
    ClientsModule.registerAsync([
      {
        name: WAITROOM_SERVICE_NAME,
        useFactory: async (config: AppConfigService) => ({
          transport: Transport.GRPC,
          options: {
            url: config.microservicesConfig.waitroomServiceUrl,
            package: WAITROOM_V1_PACKAGE_NAME,
            protoPath: join(__dirname, '../../protos', 'waitroom.proto'),
          },
        }),
        inject: [AppConfigService],
      },
    ]),
  ],
  controllers: [WaitroomController],
  providers: [WaitroomService],
})
export class WaitroomModule {}
