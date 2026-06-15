import { Module } from '@nestjs/common';
import { EventsService } from './events.service';
import { GrpcEventsController } from './controllers/grpc/events.controller';
import { PrismaModule } from '@/infra/database/prisma/prisma.module';
import { EventsRepository } from './repository/events.repository';

@Module({
  imports: [PrismaModule],
  controllers: [GrpcEventsController],
  providers: [EventsService, EventsRepository],
})
export class EventsModule {}
