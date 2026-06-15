import { Injectable } from '@nestjs/common';
import * as winston from 'winston';
import * as DailyRotateFile from 'winston-daily-rotate-file';
import * as dotenv from 'dotenv';
import { ISwaggerConfig } from '@/shared/interfaces/swagger-config.interface';
import {
  KafkaConfig,
  logLevel,
  Partitioners,
  ProducerConfig,
  RetryOptions,
  SASLOptions,
} from 'kafkajs';

@Injectable()
export class AppConfigService {
  constructor() {
    dotenv.config({
      path: `.env`,
    });

    // Replace \\n with \n to support multiline strings in AWS
    for (const envName of Object.keys(process.env)) {
      process.env[envName] = process.env[envName]?.replace(/\\n/g, '\n');
    }
  }

  public get(key: string): string {
    return process.env[key] || '';
  }

  public getNumber(key: string): number {
    return Number(this.get(key));
  }

  get nodeEnv(): string {
    return this.get('NODE_ENV') || 'development';
  }

  get databaseConfig() {
    return {
      url: this.get('DATABASE_URL'),
      host: this.get('DATABASE_HOST'),
      port: this.getNumber('DATABASE_PORT'),
      name: this.get('DATABASE_NAME'),
      username: this.get('DATABASE_USERNAME'),
      password: this.get('DATABASE_PASSWORD'),
      synchronize: this.get('DATABASE_SYNCHRONIZE'),
      logging: this.get('DATABASE_LOGGING'),
      ssl: this.get('DATABASE_SSL'),
    };
  }

  getKafkaClientConfig(): KafkaConfig {
    const brokers = (this.get('KAFKA_BROKERS') || 'kafka:29092')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);

    const retry: RetryOptions | undefined = {
      initialRetryTime: this.getNumber('KAFKA_RETRY_INITIAL_MS') || 100,
      retries: this.getNumber('KAFKA_RETRY_RETRIES') || 8,
      maxRetryTime: this.getNumber('KAFKA_RETRY_MAX_MS') || 30_000,
      factor: this.getNumber('KAFKA_RETRY_FACTOR') || 2,
    };

    const useSSL = this.get('KAFKA_SSL') === 'true';

    let sasl: SASLOptions | undefined;
    const mechanism = this.get('KAFKA_SASL_MECHANISM');
    const username = this.get('KAFKA_USERNAME');
    const password = this.get('KAFKA_PASSWORD');
    if (mechanism && username && password) {
      sasl = { mechanism, username, password } as SASLOptions;
    }

    return {
      clientId: this.get('KAFKA_CLIENT_ID') || 'app',
      brokers,
      ssl: useSSL || undefined,
      sasl,
      connectionTimeout: this.getNumber('KAFKA_CONNECTION_TIMEOUT_MS') || 3000,
      authenticationTimeout: this.getNumber('KAFKA_AUTH_TIMEOUT_MS') || 10_000,
      reauthenticationThreshold: this.getNumber('KAFKA_REAUTH_THRESHOLD_MS') || 60_000,
      requestTimeout: this.getNumber('KAFKA_REQUEST_TIMEOUT_MS') || 25_000,
      enforceRequestTimeout: this.get('KAFKA_ENFORCE_REQUEST_TIMEOUT') === 'true',
      retry,
      logLevel: logLevel.NOTHING,
    };
  }

  getKafkaProducerConfig(): ProducerConfig {
    const partitionerEnv = (this.get('KAFKA_PRODUCER_PARTITIONER') || 'legacy').toLowerCase();
    const createPartitioner =
      partitionerEnv === 'default'
        ? Partitioners.DefaultPartitioner
        : Partitioners.LegacyPartitioner;

    return {
      idempotent: this.get('KAFKA_PRODUCER_IDEMPOTENT') === 'true',
      maxInFlightRequests: this.getNumber('KAFKA_PRODUCER_MAX_IN_FLIGHT') || 5,
      transactionTimeout: this.getNumber('KAFKA_PRODUCER_TX_TIMEOUT_MS') || 30_000,
      allowAutoTopicCreation: this.get('KAFKA_PRODUCER_AUTO_TOPIC') === 'true',
      createPartitioner,
      retry: {
        initialRetryTime: this.getNumber('KAFKA_RETRY_INITIAL_MS') || 100,
        retries: this.getNumber('KAFKA_RETRY_RETRIES') || 8,
        maxRetryTime: this.getNumber('KAFKA_RETRY_MAX_MS') || 30_000,
        factor: this.getNumber('KAFKA_RETRY_FACTOR') || 2,
      },
    };
  }

  get redisConfig() {
    return {
      url: this.get('REDIS_URL') || 'redis://localhost:6379',
      host: this.get('REDIS_HOST'),
      port: this.getNumber('REDIS_PORT'),
      password: this.get('REDIS_PASSWORD'),
      retryDelayOnFailover: this.getNumber('REDIS_RETRY_DELAY_ON_FAILOVER'),
      enableReadyCheck: this.get('REDIS_ENABLE_READY_CHECK'),
      maxRetriesPerRequest: this.getNumber('REDIS_MAX_RETRIES_PER_REQUEST'),
    };
  }

  get microservicesConfig() {
    return {
      internalKey: this.get('MICROSERVICE_INTERNAL_KEY'),
      transport: {
        tcp: this.get('MICROSERVICES_TRANSPORT_TCP'),
        redis: this.get('MICROSERVICES_TRANSPORT_REDIS'),
        rabbitmq: this.get('MICROSERVICES_TRANSPORT_RABBITMQ'),
      },
      auth: this.get('MICROSERVICES_AUTH'),
      events: this.get('MICROSERVICES_EVENTS'),
      users: this.get('MICROSERVICES_USERS'),
    };
  }

  get swaggerConfig(): ISwaggerConfig {
    return {
      path: this.get('SWAGGER_PATH') || 'docs',
      title: this.get('SWAGGER_TITLE') || 'TicketBottle Event API',
      description: this.get('SWAGGER_DESCRIPTION'),
      version: this.get('SWAGGER_VERSION') || '0.0.1',
      scheme: this.get('SWAGGER_SCHEME') === 'https' ? 'https' : 'http',
    };
  }

  get zalopayConfig() {
    return {
      appID: this.getNumber('ZALOPAY_APP_ID'),
      key1: this.get('ZALOPAY_KEY1'),
      key2: this.get('ZALOPAY_KEY2'),
    };
  }

  get payosConfig() {
    return {
      clientId: this.get('PAYOS_CLIENT_ID'),
      apiKey: this.get('PAYOS_API_KEY'),
      checksumKey: this.get('PAYOS_CHECKSUM_KEY'),
    };
  }

  get winstonConfig() {
    return {
      transports: [
        new DailyRotateFile({
          level: 'debug',
          filename: `./logs/${this.nodeEnv}/debug-%DATE%.log`,
          datePattern: 'YYYY-MM-DD',
          zippedArchive: true,
          maxSize: '20m',
          maxFiles: '14d',
          format: winston.format.combine(winston.format.timestamp(), winston.format.json()),
        }),
        new DailyRotateFile({
          level: 'error',
          filename: `./logs/${this.nodeEnv}/error-%DATE%.log`,
          datePattern: 'YYYY-MM-DD',
          zippedArchive: false,
          maxSize: '20m',
          maxFiles: '30d',
          format: winston.format.combine(winston.format.timestamp(), winston.format.json()),
        }),
        new winston.transports.Console({
          level: 'debug',
          handleExceptions: true,
          format: winston.format.combine(
            winston.format.colorize(),
            winston.format.timestamp({
              format: 'DD-MM-YYYY HH:mm:ss',
            }),
            winston.format.printf(({ level, message, timestamp, context, trace }) => {
              const ctx = context ? ` [${context}]` : '';
              const msgStr = typeof message === 'string' ? message : JSON.stringify(message);
              const stackStr = trace ? `\n${trace}` : '';
              return `${timestamp} ${level}:${ctx} ${msgStr}${stackStr}`;
            }),
          ),
        }),
      ],
      exitOnError: false,
    };
  }

  get appConfig() {
    return {
      host: this.get('HOST'),
      name: this.get('NAME'),
      version: this.get('VERSION'),
      port: this.getNumber('APP_PORT'),
      globalPrefix: this.get('APP_GLOBAL_PREFIX'),
      corsOrigins: this.get('CORS_ORIGINS'),
      logLevel: this.get('LOG_LEVEL'),
      encryptKey: this.get('ENCRYPT_KEY'),
      webhookBaseUrl: this.get('WEBHOOK_BASE_URL'),
    };
  }
}
