import { ConsoleLogger, Injectable } from '@nestjs/common';
import * as winston from 'winston';
import { AppConfigService } from './config.service';

@Injectable()
export class LoggerService extends ConsoleLogger {
  private readonly logger: winston.Logger;

  constructor(private readonly configService: AppConfigService) {
    super(LoggerService.name, { timestamp: true });
    this.logger = winston.createLogger(configService.winstonConfig);
    if (this.configService.nodeEnv !== 'production') {
      this.logger.debug('Logging initialized at debug level');
    }
  }

  error(message: string, trace?: string, context?: string): void {
    this.logger.error(message, { trace, context });
  }
  log(message: string, context?: string) {
    this.logger.info(message, context);
  }
  info(message: string, context?: string) {
    this.logger.info(message, context);
  }
  debug(message: string, context?: string) {
    this.logger.debug(message, context);
  }
  warn(message: string, context?: string) {
    this.logger.warn(message, context);
  }
}
