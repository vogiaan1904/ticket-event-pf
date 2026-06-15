import { USER_PACKAGE_NAME, USER_SERVICE_NAME } from '@/protogen/user.pb';
import { AppConfigService } from '@/shared/services/config.service';
import { RedisModule } from '@nestjs-modules/ioredis';
import { Module } from '@nestjs/common';
import { JwtModule } from '@nestjs/jwt';
import { ClientsModule, Transport } from '@nestjs/microservices';
import { join } from 'path';
import { AuthController } from './auth.controller';
import { AuthService } from './auth.service';
import { JwtStrategy } from './strategies/jwt.strategy';

@Module({
  imports: [
    JwtModule.registerAsync({
      useFactory: async (configService: AppConfigService) => ({
        secret: configService.appConfig.jwtSecret,
        signOptions: {
          algorithm: 'HS256',
          expiresIn: configService.appConfig.jwtAccessExpiration,
        },
      }),
      inject: [AppConfigService],
    }),
    RedisModule.forRootAsync({
      useFactory: async (configService: AppConfigService) => ({
        type: 'single',
        url: configService.redisConfig.url,
      }),
      inject: [AppConfigService],
    }),
    ClientsModule.registerAsync([
      {
        name: USER_SERVICE_NAME,
        useFactory: async (configService: AppConfigService) => ({
          transport: Transport.GRPC,
          options: {
            url: configService.microservicesConfig.userServiceUrl,
            package: USER_PACKAGE_NAME,
            protoPath: join(__dirname, '../../protos', 'user.proto'),
          },
        }),
        inject: [AppConfigService],
      },
    ]),
  ],
  controllers: [AuthController],
  providers: [AuthService, JwtStrategy],
})
export class AuthModule {}
