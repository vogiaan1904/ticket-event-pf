import { EventType } from './event.types';

export type { Outbox as OutboxEntity } from '@prisma/client';

export interface TypedOutboxEvent<T = any> {
  id: string;
  aggregateId: string;
  aggregateType: string;
  eventType: EventType;
  payload: T;
  published: boolean;
  publishedAt: Date | null;
  createdAt: Date;
  retryCount: number;
  lastError: string | null;
}
