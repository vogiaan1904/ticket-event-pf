import { PrismaClient } from '@prisma/client';
import { logger } from '../logger';

let prisma: PrismaClient | null = null;

export const getPrismaClient = (): PrismaClient => {
  if (!prisma) {
    logger.info('Initializing Prisma client');

    prisma = new PrismaClient({
      log:
        process.env.LOG_LEVEL === 'debug' ? ['query', 'info', 'warn', 'error'] : ['warn', 'error'],
      errorFormat: 'minimal',
    });

    prisma.$connect().catch((error: any) => {
      logger.error('Failed to connect to database', { error: error.message });
      prisma = null;
      throw error;
    });

    logger.info('Prisma client initialized successfully');
  }

  return prisma;
};

export const disconnectPrisma = async (): Promise<void> => {
  if (prisma) {
    logger.info('Disconnecting Prisma client');
    await prisma.$disconnect();
    prisma = null;
  }
};

export type { PrismaClient } from '@prisma/client';
