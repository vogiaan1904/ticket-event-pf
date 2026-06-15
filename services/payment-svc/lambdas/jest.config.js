module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  roots: ['<rootDir>'],
  testMatch: ['**/__tests__/**/*.test.ts', '**/*.test.ts'],
  moduleFileExtensions: ['ts', 'js', 'json'],
  moduleNameMapper: {
    '^@/common/(.*)$': '<rootDir>/common/$1',
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
