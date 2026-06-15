import { PrismaModule } from '@/shared/prisma/prisma.module';
import { Module } from '@nestjs/common';
import { UserController } from './user.controller';
import { UserServiceImpl } from './user.service';

@Module({
  imports: [PrismaModule],
  controllers: [UserController],
  providers: [{ provide: 'UserService', useClass: UserServiceImpl }],
})
export class UserModule {}
