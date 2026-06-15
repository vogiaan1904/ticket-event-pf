import { Timestamp } from '@/protogen/google/protobuf/timestamp.pb';

export class TimestampUtil {
  static fromDate(date: Date): Timestamp {
    const millis = date.getTime();
    return {
      seconds: Math.floor(millis / 1000),
      nanos: (millis % 1000) * 1000000,
    };
  }

  static toDate(timestamp: Timestamp): Date {
    const seconds = timestamp.seconds;
    return new Date(seconds * 1000 + timestamp.nanos / 1000000);
  }
}
