import winston from 'winston';

export const logger = winston.createLogger({
  level: process.env.LOG_LEVEL || 'info',
  format: winston.format.combine(
    winston.format.timestamp({ format: 'YYYY-MM-DD HH:mm:ss' }),
    winston.format.errors({ stack: true }),
    winston.format.json()
  ),
  defaultMeta: {
    service: 'payment-lambda',
    environment: process.env.NODE_ENV || 'development',
  },
  transports: [
    new winston.transports.Console({
      format: winston.format.combine(
        winston.format.colorize(),
        winston.format.printf(({ timestamp, level, message, ...meta }) => {
          const metaStr = Object.keys(meta).length ? JSON.stringify(meta) : '';
          return `${timestamp} [${level}]: ${message} ${metaStr}`;
        })
      ),
    }),
  ],
});

export const logError = (message: string, error: Error, meta?: Record<string, any>): void => {
  logger.error(message, {
    error: {
      name: error.name,
      message: error.message,
      stack: error.stack,
    },
    ...meta,
  });
};

export const logWarn = (message: string, meta?: Record<string, any>): void => {
  logger.warn(message, meta);
};

export const logInfo = (message: string, meta?: Record<string, any>): void => {
  logger.info(message, meta);
};

export const logDebug = (message: string, meta?: Record<string, any>): void => {
  logger.debug(message, meta);
};

export default logger;
