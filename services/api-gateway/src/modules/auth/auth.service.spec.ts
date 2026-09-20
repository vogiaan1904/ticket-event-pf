import { JwtService } from '@nestjs/jwt';
import { AppConfigService } from '@services/config.service';
import { createHash } from 'crypto';
import { AuthService } from './auth.service';

function buildService() {
  const calls: Array<[string, unknown[]]> = [];
  const multi = {
    setex: (...a: unknown[]) => (calls.push(['setex', a]), multi),
    sadd: (...a: unknown[]) => (calls.push(['sadd', a]), multi),
    expire: (...a: unknown[]) => (calls.push(['expire', a]), multi),
    del: (...a: unknown[]) => (calls.push(['del', a]), multi),
    srem: (...a: unknown[]) => (calls.push(['srem', a]), multi),
    incr: (...a: unknown[]) => (calls.push(['incr', a]), multi),
    exec: async () => [],
  };
  const redis = {
    get: jest.fn().mockResolvedValue(null),
    smembers: jest.fn().mockResolvedValue([]),
    multi: () => multi,
  };
  const jwt = { sign: jest.fn().mockReturnValue('access-token') } as unknown as JwtService;
  const config = {
    appConfig: {
      jwtAccessExpiration: '1d',
      jwtRefreshExpiration: '7d',
      jwtRefreshSlidingWindow: '24h',
    },
  } as AppConfigService;
  const grpcClient = { getService: jest.fn().mockReturnValue({}) };

  const service = new AuthService(grpcClient as never, config, jwt, redis as never);
  service.onModuleInit();
  return { service, calls, redis };
}

describe('AuthService', () => {
  it('never writes a refresh token into Redis in the clear', async () => {
    const { service, calls } = buildService();

    const { refreshToken } = await service.generateTokenPair('user-1', 'a@b.c');

    // The token is the bearer credential. A Redis dump must not be a session dump.
    const written = JSON.stringify(calls);
    expect(written).not.toContain(refreshToken);
    expect(written).toContain(createHash('sha256').update(refreshToken).digest('hex'));
  });

  it('deletes the keys it actually wrote when a user is signed out everywhere', async () => {
    const { service, calls, redis } = buildService();
    const { refreshToken } = await service.generateTokenPair('user-1', 'a@b.c');
    const digest = createHash('sha256').update(refreshToken).digest('hex');
    redis.smembers.mockResolvedValue([digest]);
    calls.length = 0;

    await service.invalidateAllUserTokens('user-1');

    expect(calls).toContainEqual(['del', [`refresh_token:${digest}`]]);
  });
});
