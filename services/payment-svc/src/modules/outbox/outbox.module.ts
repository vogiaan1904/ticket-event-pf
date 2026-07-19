import { DynamicModule, Module } from '@nestjs/common';
import { OutboxService } from './outbox.service';
import { PrismaModule } from '../../infra/database/prisma/prisma.module';

@Module({})
export class OutboxModule {
  static forRoot(): DynamicModule {
    return {
      module: OutboxModule,
      imports: [PrismaModule],
      providers: [OutboxService],
      exports: [OutboxService],
    };
  }
}
