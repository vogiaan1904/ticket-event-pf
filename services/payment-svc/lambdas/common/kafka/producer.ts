import { Kafka, Producer, ProducerRecord, RecordMetadata } from 'kafkajs';
import { logger } from '../logger';

let producer: Producer | null = null;
let isConnected = false;

export const getKafkaProducer = async (): Promise<Producer> => {
  if (!producer || !isConnected) {
    const brokers = process.env.KAFKA_BROKERS?.split(',').map((b) => b.trim()) || [];
    if (brokers.length === 0) {
      throw new Error('KAFKA_BROKERS environment variable is required');
    }

    const kafka = new Kafka({
      clientId: 'payment-lambda',
      brokers,
      ssl: process.env.KAFKA_SSL === 'true',
      sasl: process.env.KAFKA_USERNAME
        ? {
            mechanism: 'plain',
            username: process.env.KAFKA_USERNAME,
            password: process.env.KAFKA_PASSWORD || '',
          }
        : undefined,
      connectionTimeout: 3000,
      requestTimeout: 25000,
      retry: {
        initialRetryTime: 100,
        retries: 8,
        maxRetryTime: 30000,
        factor: 2,
      },
    });

    producer = kafka.producer({
      idempotent: true,
      maxInFlightRequests: 5,
      transactionTimeout: 30000,
    });

    try {
      await producer.connect();
      isConnected = true;
    } catch (error) {
      logger.error('Failed to connect Kafka producer', { error });
      producer = null;
      isConnected = false;
      throw error;
    }
  }

  return producer;
};

export const publishToKafka = async <T = any>(
  topic: string,
  value: T,
  key?: string,
  headers?: Record<string, string>,
): Promise<RecordMetadata[]> => {
  const producer = await getKafkaProducer();

  const record: ProducerRecord = {
    topic,
    acks: -1, // Wait for all in-sync replicas
    timeout: 30000,
    messages: [
      {
        key: key ? Buffer.from(key) : undefined,
        value: Buffer.from(JSON.stringify(value)),
        headers: headers
          ? Object.entries(headers).reduce(
              (acc, [k, v]) => {
                acc[k] = Buffer.from(v);
                return acc;
              },
              {} as Record<string, Buffer>,
            )
          : undefined,
      },
    ],
  };

  try {
    const metadata = await producer.send(record);
    return metadata;
  } catch (error) {
    logger.error('Failed to publish message to Kafka', { topic, error });
    throw error;
  }
};

export const disconnectKafka = async (): Promise<void> => {
  if (producer && isConnected) {
    await producer.disconnect();
    producer = null;
    isConnected = false;
  }
};

export const publishWithRetry = async <T = any>(
  topic: string,
  value: T,
  key?: string,
  headers?: Record<string, string>,
  maxRetries: number = 3,
): Promise<RecordMetadata[]> => {
  let lastError: Error | null = null;

  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      return await publishToKafka(topic, value, key, headers);
    } catch (error) {
      lastError = error as Error;
      logger.warn(`Kafka publish attempt ${attempt}/${maxRetries} failed`, {
        topic,
        error: (error as Error).message,
      });

      if (attempt < maxRetries) {
        // Exponential backoff
        const backoffMs = Math.min(1000 * Math.pow(2, attempt - 1), 10000);
        await new Promise((resolve) => setTimeout(resolve, backoffMs));
      }
    }
  }

  throw new Error(`Failed to publish to Kafka after ${maxRetries} attempts: ${lastError?.message}`);
};

// Export Kafka types for convenience
export type { Producer, ProducerRecord, RecordMetadata } from 'kafkajs';
