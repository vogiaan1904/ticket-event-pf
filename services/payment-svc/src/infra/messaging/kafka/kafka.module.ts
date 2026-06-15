// src/kafka/kafka.module.ts
import { Global, Module, DynamicModule } from '@nestjs/common';
import { Kafka, KafkaConfig, Producer, ProducerConfig } from 'kafkajs';
import { AppConfigService } from '@/shared/services/config.service';
import { KAFKA, KAFKA_PRODUCER } from './kafka.tokens';
import { KafkaProducerService } from './kafka-producer.service';

@Global() // make it available app-wide without importing everywhere
@Module({})
export class KafkaModule {
  static forRootAsync(): DynamicModule {
    return {
      module: KafkaModule,
      providers: [
        {
          provide: KAFKA,
          inject: [AppConfigService],
          useFactory: (cfg: AppConfigService) =>
            new Kafka(cfg.getKafkaClientConfig() as KafkaConfig),
        },
        {
          provide: KAFKA_PRODUCER,
          inject: [KAFKA, AppConfigService],
          useFactory: async (kafka: Kafka, cfg: AppConfigService): Promise<Producer> => {
            const prodCfg = cfg.getKafkaProducerConfig() as ProducerConfig;
            const producer = kafka.producer(prodCfg);
            // small connect retry to avoid race with broker start
            const deadline = Date.now() + 30_000;
            // eslint-disable-next-line no-constant-condition
            while (true) {
              try {
                await producer.connect();
                break;
              } catch (e) {
                if (Date.now() > deadline) throw e;
                await new Promise((r) => setTimeout(r, 750));
              }
            }
            return producer;
          },
        },
        KafkaProducerService,
      ],
      exports: [KafkaProducerService],
      global: true,
    };
  }
}
