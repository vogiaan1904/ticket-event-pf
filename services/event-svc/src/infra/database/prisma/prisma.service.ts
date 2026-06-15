import { LoggerService } from '@/shared/services/logger.service';
import {
  Injectable,
  InternalServerErrorException,
  OnModuleDestroy,
  OnModuleInit,
} from '@nestjs/common';
import { PrismaClient } from '@prisma/client';

@Injectable()
export class PrismaService extends PrismaClient implements OnModuleInit, OnModuleDestroy {
  constructor(private readonly logger: LoggerService) {
    super();
  }
  async onModuleInit() {
    try {
      await this.$connect();
      this.logger.info('Connected to database');
    } catch (error) {
      this.logger.error('Failed to connect to database.', error);
      throw new InternalServerErrorException(error);
    }
  }

  async onModuleDestroy() {
    await this.$disconnect();
  }
}
