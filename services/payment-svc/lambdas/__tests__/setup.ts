// Set test environment variables
process.env.NODE_ENV = 'test';
process.env.LOG_LEVEL = 'error'; // Reduce noise in test output
process.env.DATABASE_URL = 'postgresql://test:test@localhost:5432/test_db';
process.env.KAFKA_BROKERS = 'localhost:9092';
process.env.KAFKA_SSL = 'false';
process.env.OUTBOX_BATCH_SIZE = '10';
process.env.OUTBOX_MAX_RETRIES = '3';
process.env.OUTBOX_RETENTION_DAYS = '7';
process.env.ZALOPAY_APP_ID = 'test_app_id';
process.env.ZALOPAY_KEY1 = 'test_key1';
process.env.ZALOPAY_KEY2 = 'test_key2';
process.env.PAYOS_CLIENT_ID = 'test_client_id';
process.env.PAYOS_API_KEY = 'test_api_key';
process.env.PAYOS_CHECKSUM_KEY = 'test_checksum_key';

// Mock console methods to reduce noise
global.console = {
  ...console,
  log: jest.fn(),
  debug: jest.fn(),
  info: jest.fn(),
  warn: jest.fn(),
  error: jest.fn(),
};
