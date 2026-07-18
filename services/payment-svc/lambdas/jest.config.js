module.exports = {
  testEnvironment: 'node',
  roots: ['<rootDir>'],
  testMatch: ['**/__tests__/**/*.test.ts', '**/*.test.ts'],
  moduleFileExtensions: ['ts', 'js', 'json'],
  moduleNameMapper: {
    '^@/common/(.*)$': '<rootDir>/common/$1',
  },
  // Integration tests share the live payments/outbox tables (non-isolated
  // beforeEach seeds), so they must run serially to stay deterministic.
  maxWorkers: 1,
  // strictNullChecks is off in tsconfig.json for legacy Prisma code; Kysely's
  // optional-insert-key inference (Generated<T>, nullable ColumnType) requires it on,
  // so tests type-check under it independently of the production build.
  transform: {
    '^.+\\.tsx?$': ['ts-jest', { tsconfig: { strictNullChecks: true } }],
  },
  collectCoverageFrom: [
    '**/*.ts',
    '!**/*.d.ts',
    '!**/node_modules/**',
    '!**/__tests__/**',
    '!**/dist/**',
  ],
  coverageDirectory: 'coverage',
  coverageReporters: ['text', 'lcov', 'html'],
  verbose: true,
  testTimeout: 10000,
};
