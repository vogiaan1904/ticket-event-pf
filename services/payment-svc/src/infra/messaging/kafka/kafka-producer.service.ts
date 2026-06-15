import { Inject, Injectable, OnModuleDestroy } from '@nestjs/common';
import { Producer, IHeaders } from 'kafkajs';
import { KAFKA_PRODUCER } from './kafka.tokens';

export interface PublishResult {
  topic: string;
  partition: number;
  offset: string;
}

@Injectable()
export class KafkaProducerService implements OnModuleDestroy {
  constructor(@Inject(KAFKA_PRODUCER) private readonly producer: Producer) {}

  async publish<T>(
    topic: string,
    value: T,
    key?: string,
    headers?: Record<string, string>,
  ): Promise<PublishResult> {
    const [res] = await this.producer.send({
      topic,
      acks: -1, // ← broker ACK from all ISR; awaited
      timeout: 30_000,
      messages: [{ key, value: Buffer.from(JSON.stringify(value)), headers: headers as IHeaders }],
    });
    return { topic: res.topicName, partition: res.partition, offset: res.baseOffset ?? '-1' };
  }

  async onModuleDestroy() {
    try {
      await this.producer.disconnect();
    } catch {}
  }
}
