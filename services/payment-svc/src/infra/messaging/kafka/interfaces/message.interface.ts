export interface IKafkaMessage<T = any> {
  key?: string;
  value: T;
  headers?: Record<string, string>;
  partition?: number;
  timestamp?: string;
}

export interface IKafkaProducerRecord<T = any> {
  topic: string;
  messages: IKafkaMessage<T>[];
}

export interface IKafkaResponse {
  topicName: string;
  partition: number;
  errorCode: number;
  offset?: string;
  timestamp?: string;
  baseOffset?: string;
  logAppendTime?: string;
  logStartOffset?: string;
}

export interface IKafkaMetadata {
  correlationId?: string;
  causationId?: string;
  eventType: string;
  eventVersion: string;
  timestamp: string;
  source: string;
}
