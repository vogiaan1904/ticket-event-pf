import { Module } from '@nestjs/common';
import { UsersService } from './users.service';
import { UsersController } from './users.controller';
import { ClientsModule, Transport } from '@nestjs/microservices';
import { USER_PACKAGE_NAME, USER_SERVICE_NAME } from '@/protogen/user.pb';
import { AppConfigService } from '@/shared/services/config.service';
import { join } from 'path';

@Module({
  imports: [
    ClientsModule.registerAsync([
      {
        name: USER_SERVICE_NAME,
        useFactory: async (config: AppConfigService) => ({
          transport: Transport.GRPC,
          options: {
            url: config.microservicesConfig.userServiceUrl,
            package: USER_PACKAGE_NAME,
            protoPath: join(__dirname, '../../protos', 'user.proto'),
          },
        }),
        inject: [AppConfigService],
      },
    ]),
  ],
  controllers: [UsersController],
  providers: [UsersService],
})
export class UsersModule {}
