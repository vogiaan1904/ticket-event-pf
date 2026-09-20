# api-gateway Test Coverage Implementation Plan

**Status:** Tasks 1-3 done. Task 4 open.

**Goal:** Put the gateway's error contract and its refresh-token storage under test, and fix the two defects the tests expose.

**Architecture:** Unit tests with mocked collaborators under `src/`, run by the jest config already in `package.json`. The gateway's highest-value logic is not a controller — it is the one place the error taxonomy becomes an HTTP status, and the one place a credential is persisted. Both are reachable without a network, a database or Redis.

**Tech Stack:** jest 29, ts-jest 29, `@nestjs/testing` 11 — already installed.

**Spec:** the ranked backlog in the 2026-09-16 scope pivot, item 2. `payment-svc` was done first and is complete; see `docs/plans/2026-09-20-payment-svc-test-coverage.md` for the shape this follows.

## Scope

**This plan covers `services/api-gateway/src/` only.** `user-svc` and `event-svc` get their own plans.

The gateway goes second because it owns two contracts nothing else can: root `CLAUDE.md` says the gRPC→HTTP mapping lives in `common/filters/global-exception.filter.ts` **"and nowhere else"**, and the gateway is the only service that mints and stores user credentials.

## Global Constraints

Carried forward from the payment-svc plan — each of these cost a task there:

- **ts-jest type-checks.** A type error fails the entire suite, not one test.
- **Prettier is an eslint error** (`plugin:prettier/recommended`). Snippets below are not pre-formatted; reflow as you paste and verify with `npx prettier --check <files>`.
- **`npm run lint` is broken in all four TS services** — ESLint 9 installed against an `.eslintrc.js` it cannot read. Prettier is the only formatting gate that runs. Do not add a lint step to CI.
- **Never cast a DTO or payload `as any` in a test.** Supply every property. A cast suppresses exactly the check that catches a wrong enum member.
- **Error taxonomy** (root `CLAUDE.md`, binding): `INTERNAL` means we have a bug and pages on any sustained rate above zero. `FAILED_PRECONDITION` never pages.
- **Metric contract** (root `CLAUDE.md`, binding): every workload publishes `tb_grpc_requests_total` labelled `service`/`method`/`code`, and the gateway records `code` too even though it serves HTTP.
- **Comment budget:** 3 lines inline, 5 on a symbol, 8 for a file header. No paragraphs.
- **Commit messages describe the platform.** No plan, phase or task numbers.
- Run every command from `services/api-gateway/`.

## File Structure

| File | Responsibility | Task |
|---|---|---|
| `package.json` | **Modify.** The `jest.moduleNameMapper` for 15 tsconfig aliases. | 1 |
| `src/common/filters/global-exception.filter.spec.ts` | **Create.** The taxonomy's HTTP mapping, pinned. | 1 |
| `src/shared/metrics/code.spec.ts` | **Create.** The metric label must agree with the status sent. | 2 |
| `src/shared/metrics/code.ts` | **Modify.** Owns both directions of the mapping; unmapped codes label `INTERNAL`. | 2 |
| `src/common/filters/global-exception.filter.ts` | **Modify.** Imports `GRPC_TO_HTTP` instead of declaring it. | 2 |
| `src/modules/auth/auth.service.spec.ts` | **Create.** Refresh tokens are never stored in the clear. | 3 |
| `src/modules/auth/auth.service.ts` | **Modify.** Index refresh tokens by digest. | 3 |
| `.github/workflows/ts-tests.yml` | **Modify.** Add the gateway as a second job. | 4 |

---

### Task 1: Make `src/` testable, and pin the taxonomy's HTTP mapping

`npm test` reports `Pattern: - 0 matches` — `rootDir` is `src`, `testRegex` is `.*\.spec\.ts$`, and no spec exists. The config also has **no `moduleNameMapper`**, so the first test importing `@services/...` fails to resolve before asserting anything. The gateway has **15** aliases, not payment's 7.

The first thing under test is the mapping itself. It is currently **correct** — all nine taxonomy codes map to the documented status, and `INTERNAL` is deliberately absent so it falls through to 500. There is no bug here; the point is that root `CLAUDE.md` calls this table binding and nothing enforces it.

**Files:**
- Modify: `package.json` (the `jest` block)
- Create: `src/common/filters/global-exception.filter.spec.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `buildFilter()` in the spec, returning `{ filter, host, response, json, request }`. `host` is the `ArgumentsHost` every case passes to `filter.catch`; `response` has a chainable `status()` and a `json()` spy; `request` is the plain object the filter writes `TB_CODE` onto. Task 2 does **not** reuse it — it tests `grpcCodeOf` directly.

- [x] **Step 1: Teach jest the path aliases**

In `package.json`, inside the `jest` object, after `"rootDir": "src",`:

```json
    "moduleNameMapper": {
      "^@/(.*)$": "<rootDir>/$1",
      "^@modules/(.*)$": "<rootDir>/modules/$1",
      "^@infra/(.*)$": "<rootDir>/infra/$1",
      "^@interfaces/(.*)$": "<rootDir>/shared/interfaces/$1",
      "^@services/(.*)$": "<rootDir>/shared/services/$1",
      "^@constants/(.*)$": "<rootDir>/shared/constants/$1",
      "^@utils/(.*)$": "<rootDir>/shared/utils/$1",
      "^@repositories/(.*)$": "<rootDir>/shared/repositories/$1",
      "^@decorators/(.*)$": "<rootDir>/common/decorators/$1",
      "^@filters/(.*)$": "<rootDir>/common/filters/$1",
      "^@guards/(.*)$": "<rootDir>/common/guards/$1",
      "^@interceptors/(.*)$": "<rootDir>/common/interceptors/$1",
      "^@middlewares/(.*)$": "<rootDir>/common/middlewares/$1",
      "^@exceptions/(.*)$": "<rootDir>/common/exceptions/$1",
      "^@protogen/(.*)$": "<rootDir>/protogen/$1"
    },
```

`rootDir` is `src`, so targets are relative to `src/`. All 15 mirror `tsconfig.json`'s `paths`; a mapper that drifts from tsconfig fails only at test time.

- [x] **Step 2: Write the failing test**

Create `src/common/filters/global-exception.filter.spec.ts`:

```ts
import { status as GrpcStatus } from '@grpc/grpc-js';
import { ArgumentsHost, HttpStatus } from '@nestjs/common';
import { AppConfigService } from '@services/config.service';
import { LoggerService } from '@services/logger.service';
import { GlobalExceptionFilter } from './global-exception.filter';

function buildFilter() {
  const json = jest.fn();
  const response = { status: jest.fn().mockReturnThis(), json };
  const request: Record<string | symbol, unknown> = {};
  const host = {
    switchToHttp: () => ({ getResponse: () => response, getRequest: () => request }),
  } as unknown as ArgumentsHost;

  const config = { isDev: false } as AppConfigService;
  const logger = { setContext: jest.fn(), error: jest.fn() } as unknown as LoggerService;

  return { filter: new GlobalExceptionFilter(config, logger), host, response, json, request };
}

// The taxonomy in the root CLAUDE.md, which this filter is the only
// implementation of. A change here is a change to the platform's contract.
const TAXONOMY: ReadonlyArray<[GrpcStatus, HttpStatus]> = [
  [GrpcStatus.INVALID_ARGUMENT, HttpStatus.BAD_REQUEST],
  [GrpcStatus.UNAUTHENTICATED, HttpStatus.UNAUTHORIZED],
  [GrpcStatus.PERMISSION_DENIED, HttpStatus.FORBIDDEN],
  [GrpcStatus.NOT_FOUND, HttpStatus.NOT_FOUND],
  [GrpcStatus.ALREADY_EXISTS, HttpStatus.CONFLICT],
  [GrpcStatus.FAILED_PRECONDITION, HttpStatus.CONFLICT],
  [GrpcStatus.UNAVAILABLE, HttpStatus.SERVICE_UNAVAILABLE],
  [GrpcStatus.DEADLINE_EXCEEDED, HttpStatus.GATEWAY_TIMEOUT],
];

describe('GlobalExceptionFilter', () => {
  it.each(TAXONOMY)('maps gRPC %i to HTTP %i', (grpcCode, httpStatus) => {
    const { filter, host, response } = buildFilter();

    filter.catch({ code: grpcCode, message: 'downstream said no' }, host);

    expect(response.status).toHaveBeenCalledWith(httpStatus);
  });

  it('answers 500 for INTERNAL, which is the one code that means our bug', () => {
    const { filter, host, response } = buildFilter();

    filter.catch({ code: GrpcStatus.INTERNAL, message: 'boom' }, host);

    expect(response.status).toHaveBeenCalledWith(HttpStatus.INTERNAL_SERVER_ERROR);
  });
});
```

- [x] **Step 3: Run the test to verify it runs at all**

```bash
npm test
```

Expected: **9 passing.** This task pins behaviour that is already correct, so unlike every payment-svc task there is no red phase. If a case fails, the filter has drifted from the taxonomy and that is the finding — report it rather than editing the test to match.

A resolution error naming `@services/...` means Step 1's mapper is wrong.

- [x] **Step 4: Commit**

```bash
git add package.json src/common/filters/global-exception.filter.spec.ts
git commit -m "test(gateway): pin the gRPC-to-HTTP mapping to the taxonomy

The root CLAUDE.md calls this table binding and names this filter its only
implementation, but nothing enforced either claim. The cases are the taxonomy
verbatim, so a silent edit to the table now fails the build instead of changing
what a client is told.

The jest config gains the tsconfig path aliases; without them no test under
src/ can resolve an import."
```

---

### Task 2: The metric label must agree with the status actually sent

`grpcCodeOf` labels every failed request for `tb_grpc_requests_total`, and its own comment calls it "the inverse of `GRPC_TO_HTTP`". It is not. For a downstream numeric code it returns `GrpcStatus[code]` unconditionally, while the filter answers **500** for any code absent from `GRPC_TO_HTTP`.

So a downstream `RESOURCE_EXHAUSTED`, `CANCELLED`, `UNKNOWN`, `DATA_LOSS` or `OK` produces an HTTP 500 — which by the taxonomy means *we have a bug* — labelled with a code that is not `INTERNAL`. The alert is `sum(tb:grpc_requests:rate5m{code="INTERNAL"}) > 0` (`deploy/helm/ticketbottle/templates/apps/prometheusrule.yaml:39`), so **the page never fires for a 500 we served.**

**The map is the right test, not the taxonomy.** `GRPC_TO_HTTP` carries 11 entries while the taxonomy names 9 codes — `OUT_OF_RANGE`, `ABORTED` and `UNIMPLEMENTED` are mapped without appearing in the binding table. Those three keep their own label, and correctly so: the filter gives each a non-500 status, so none of them is a fault we must answer for. The rule is "did the filter map it", never "is it in the taxonomy".

The two maps must live in one module for this to stay true. `code.ts` already owns `HTTP_TO_GRPC` and the filter already imports from it, so `GRPC_TO_HTTP` moves there and the filter imports it — the dependency direction is unchanged and no cycle appears.

**Files:**
- Create: `src/shared/metrics/code.spec.ts`
- Modify: `src/shared/metrics/code.ts`
- Modify: `src/common/filters/global-exception.filter.ts:13-25` (the map moves out) and its import block

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces: `GRPC_TO_HTTP` exported from `@/shared/metrics/code`, typed `Partial<Record<GrpcStatus, HttpStatus>>`.

- [x] **Step 1: Write the failing test**

Create `src/shared/metrics/code.spec.ts`:

```ts
import { status as GrpcStatus } from '@grpc/grpc-js';
import { BadRequestException, InternalServerErrorException } from '@nestjs/common';
import { grpcCodeOf } from './code';

describe('grpcCodeOf', () => {
  it('labels a downstream code the filter maps with that code', () => {
    expect(grpcCodeOf({ code: GrpcStatus.NOT_FOUND })).toBe('NOT_FOUND');
  });

  // The filter answers 500 for any code absent from GRPC_TO_HTTP. Labelling
  // that request anything but INTERNAL hides it from the only alert that pages.
  it.each([
    GrpcStatus.RESOURCE_EXHAUSTED,
    GrpcStatus.CANCELLED,
    GrpcStatus.UNKNOWN,
    GrpcStatus.DATA_LOSS,
    GrpcStatus.OK,
  ])('labels an unmapped downstream code %i as INTERNAL', (code) => {
    expect(grpcCodeOf({ code })).toBe('INTERNAL');
  });

  it('labels a gateway HttpException with the code its status came from', () => {
    expect(grpcCodeOf(new BadRequestException())).toBe('INVALID_ARGUMENT');
  });

  it('labels a gateway 500 as INTERNAL', () => {
    expect(grpcCodeOf(new InternalServerErrorException())).toBe('INTERNAL');
  });

  it('labels anything else INTERNAL', () => {
    expect(grpcCodeOf(new Error('boom'))).toBe('INTERNAL');
  });
});
```

- [x] **Step 2: Run the test to verify it fails**

```bash
npm test
```

Expected: the four `it.each` cases fail — `Expected: "INTERNAL", Received: "RESOURCE_EXHAUSTED"` and its siblings. The other three pass.

- [x] **Step 3: Move the map into `code.ts`**

In `src/shared/metrics/code.ts`, add `HttpStatus` to the `@nestjs/common` import and insert above `HTTP_TO_GRPC`:

```ts
// The gateway's half of the error taxonomy: which downstream codes become a
// status of their own. A code absent here is answered 500 -- genuinely ours.
export const GRPC_TO_HTTP: Partial<Record<GrpcStatus, HttpStatus>> = {
  [GrpcStatus.INVALID_ARGUMENT]: HttpStatus.BAD_REQUEST,
  [GrpcStatus.NOT_FOUND]: HttpStatus.NOT_FOUND,
  [GrpcStatus.ALREADY_EXISTS]: HttpStatus.CONFLICT,
  [GrpcStatus.PERMISSION_DENIED]: HttpStatus.FORBIDDEN,
  [GrpcStatus.UNAUTHENTICATED]: HttpStatus.UNAUTHORIZED,
  [GrpcStatus.FAILED_PRECONDITION]: HttpStatus.CONFLICT,
  [GrpcStatus.OUT_OF_RANGE]: HttpStatus.BAD_REQUEST,
  [GrpcStatus.ABORTED]: HttpStatus.CONFLICT,
  [GrpcStatus.UNIMPLEMENTED]: HttpStatus.NOT_IMPLEMENTED,
  [GrpcStatus.UNAVAILABLE]: HttpStatus.SERVICE_UNAVAILABLE,
  [GrpcStatus.DEADLINE_EXCEEDED]: HttpStatus.GATEWAY_TIMEOUT,
};
```

Copy the entries from the filter verbatim — the point is that one list now serves both directions, not that the list changes.

- [x] **Step 4: Make the label follow the status**

In the same file, change the numeric branch of `grpcCodeOf`:

```ts
  const downstream = (err as { code?: unknown })?.code;
  if (typeof downstream === 'number') {
    // A code the filter does not map was answered 500, so INTERNAL is the
    // truthful label -- and the only one the alert watches.
    return downstream in GRPC_TO_HTTP ? GrpcStatus[downstream] : 'INTERNAL';
  }
```

- [x] **Step 5: Have the filter import the map**

In `src/common/filters/global-exception.filter.ts`, delete **lines 10-25** — the three-line rationale comment *and* the `GRPC_TO_HTTP` declaration it introduces — and add `GRPC_TO_HTTP` to the existing import from `@/shared/metrics/code`. Deleting only 13-25 orphans the comment above `interface GrpcError`. Step 3's snippet gives the map a fresh, shorter comment in its new home, so this is a rewrite rather than a move.

Also repoint `HTTP_TO_GRPC`'s own comment: it reads "the inverse of `GRPC_TO_HTTP` in `filters/global-exception.filter.ts`", a file that no longer holds the map. It becomes "above", in the same commit.

- [x] **Step 6: Run the tests to verify they pass**

```bash
npm test
```

Expected: **18 passing** across 2 suites — Task 1's 9 plus this task's 9. Task 1's cases must still pass: the filter's behaviour is unchanged, only where the table lives.

- [x] **Step 7: Commit**

```bash
git add src/shared/metrics/code.ts src/shared/metrics/code.spec.ts src/common/filters/global-exception.filter.ts
git commit -m "fix(gateway): label a request with the code it was answered with

A downstream code the filter does not map is answered 500, but the metric
recorded the downstream's own code. RESOURCE_EXHAUSTED, CANCELLED, UNKNOWN and
DATA_LOSS therefore served a 500 while labelling it something the INTERNAL alert
does not watch, so a fault the taxonomy says must page went unseen.

The two directions of the mapping now live in one module, which is what the
comment claiming they were inverses always assumed."
```

---

### Task 3: A stolen Redis dump must not be a stolen session

`generateTokenPair` stores the refresh token **as the Redis key** — `refresh_token:${token}` — and again as a raw member of the `user_tokens:${userId}` set. Anyone who can read Redis (`KEYS refresh_token:*`, or `SMEMBERS` on one user) holds every live refresh token in the clear and can mint an access token for any account.

The class already has `hashData` (argon2), used for passwords. Argon2 is salted and therefore cannot be used as a lookup key. A refresh token is 32 random bytes from `randomBytes`, so it has full entropy and needs no stretching — a SHA-256 digest is the correct index, and is what makes lookup possible.

**This invalidates every live refresh token on deploy.** Existing keys are the raw token; the new lookup hashes first, so no stored key matches and every session re-authenticates once. That is the intended cost and must be in the commit message.

**Files:**
- Create: `src/modules/auth/auth.service.spec.ts`
- Modify: `src/modules/auth/auth.service.ts` — `getRefreshTokenKey`, and the `sadd`/`srem` members

**Interfaces:**
- Consumes: nothing from Tasks 1-2.
- Produces: nothing other tasks read.

- [x] **Step 1: Write the failing test**

Create `src/modules/auth/auth.service.spec.ts`. Build the service by direct construction rather than `Test.createTestingModule`, because its constructor takes a `ClientGrpc` whose `getService` is called in `onModuleInit` — read the constructor and `onModuleInit` first and mock exactly what they touch:

```ts
import { createHash } from 'crypto';
import { JwtService } from '@nestjs/jwt';
import { AppConfigService } from '@services/config.service';
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
});
```

If the constructor or `onModuleInit` needs more than this, extend the mocks — do not change the assertion.

- [x] **Step 2: Run the test to verify it fails**

```bash
npm test
```

Expected: fails on `expect(written).not.toContain(refreshToken)` — the raw token appears in both the `setex` key and the `sadd` member.

- [x] **Step 3: Index by digest**

In `src/modules/auth/auth.service.ts`, add `createHash` to the `crypto` import and a private helper beside `generateSecureToken`:

```ts
  // Refresh tokens are 32 random bytes, so a digest needs no stretching -- and
  // unlike argon2 it is stable, which is what makes it usable as a key.
  private digest(token: string): string {
    return createHash('sha256').update(token).digest('hex');
  }
```

Change `getRefreshTokenKey` to hash what it is given, with the prefix in one place — Step 4 needs to build the same key from a digest it already has:

```ts
  // Two entry points: a raw token from a caller, a digest read back from the
  // user's set. One place spells the prefix.
  private refreshKeyOf(digest: string): string {
    return `refresh_token:${digest}`;
  }

  private getRefreshTokenKey(token: string): string {
    return this.refreshKeyOf(this.digest(token));
  }
```

Then replace the two raw-token set members. In `generateTokenPair`:

```ts
    multi.sadd(userTokensKey, this.digest(refreshToken));
```

and in `invalidateRefreshToken`:

```ts
      multi.srem(userTokensKey, this.digest(refreshToken));
```

Every caller already passes the raw token through `getRefreshTokenKey`, so no call site changes.

`changePassword` needs no edit of its own but is the worst instance of the Step 4 bug: it delegates to `invalidateAllUserTokens`, so a user changing a password *because it was compromised* would have kept every stolen session alive.

- [x] **Step 4: Stop `invalidateAllUserTokens` double-hashing**

`invalidateAllUserTokens:212` calls `this.getRefreshTokenKey(token)` on members read back from the `user_tokens:` set. After Step 3 those members are already digests, so that call hashes a digest and deletes a key that was never written — **a signed-out user would keep every working session.** This is not a check; it is a required part of the change.

Replace the loop body:

```ts
      // Members are already digests, so build the key rather than hashing again.
      refreshTokens.forEach((digest) => {
        multi.del(this.refreshKeyOf(digest));
      });
```

Add a second case to the spec proving it, before you change the code:

```ts
  it('deletes the keys it actually wrote when a user is signed out everywhere', async () => {
    const { service, calls, redis } = buildService();
    const { refreshToken } = await service.generateTokenPair('user-1', 'a@b.c');
    const digest = createHash('sha256').update(refreshToken).digest('hex');
    redis.smembers = jest.fn().mockResolvedValue([digest]);
    calls.length = 0;

    await service.invalidateAllUserTokens('user-1');

    expect(calls).toContainEqual(['del', [`refresh_token:${digest}`]]);
  });
```

`buildService()` must return `redis` for this — add it to the returned object in Step 1.

Two fixture details this depends on, both of which fail the suite if missed. `smembers` must be **declared** in the `redis` literal, not assigned onto it later: ts-jest type-checks, so assigning an undeclared property is TS2339 and kills every suite. And `multi` needs `incr`, because `invalidateAllUserTokens` calls it after the deletes — without it the case throws `multi.incr is not a function` and goes red whether or not the double-hash bug exists, which is worthless as proof.

- [x] **Step 5: Run the test to verify it passes**

```bash
npm test
```

Expected: **20 passing** across 3 suites.

- [x] **Step 6: Commit**

```bash
git add src/modules/auth/auth.service.ts src/modules/auth/auth.service.spec.ts
git commit -m "fix(gateway): index refresh tokens by digest, not by the token

The token was the Redis key and a raw member of the user's token set, so read
access to Redis was read access to every live session -- the token is a bearer
credential and was stored exactly as presented. A SHA-256 index makes a dump
useless while keeping the O(1) lookup the raw key gave; the tokens are 32 random
bytes, so there is nothing for a slow hash to protect.

Every refresh token in flight stops resolving on deploy and each session
re-authenticates once."
```

---

### Task 4: CI runs the gateway's suite too

`ts-tests.yml` runs `payment-svc` only. The gateway needs the same treatment, as a second job rather than a second workflow.

**Files:**
- Modify: `.github/workflows/ts-tests.yml`

**Interfaces:**
- Consumes: the spec files from Tasks 1-3.
- Produces: nothing.

- [ ] **Step 1: Add the job**

Read the existing file first. Extend the `paths` filters to include `services/api-gateway/**`, and add a job mirroring `payment-svc`'s:

```yaml
  api-gateway:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: services/api-gateway
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          # Matches the service Dockerfiles; @types/node is 22, the runtime is not.
          node-version: "20"

      - run: npm ci

      - run: npm test
```

**No `prisma generate` step:** the gateway has no Prisma schema — verified, `services/api-gateway/prisma` does not exist. It is a pure gRPC client.

- [ ] **Step 2: Verify from a clean clone**

```bash
cd "$(mktemp -d)" && git clone --depth 1 --branch dev file://$HOME/coding/projects/TicketEventPF r \
  && cd r/services/api-gateway && npm ci && npm test
```

Expected: **20 passed**. `--branch dev` is explicit: without it the clone follows the local repo's symbolic `HEAD`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ts-tests.yml
git commit -m "ci: run the gateway's unit tests alongside the payment service's

The gateway owns the error contract every client sees and the only credential
store in the platform, and neither was covered by anything that runs on a push."
```

---

## Done when

- [ ] `npm test` in `services/api-gateway` reports 20 passing tests across 3 suites.
- [ ] The same passes from a clean `npm ci` in a fresh clone.
- [ ] `ts-tests` is green on `dev` for both jobs.
- [ ] No test requires Redis, a database or the network.
- [ ] No gRPC code answered 500 is labelled anything but `INTERNAL`.

## Not in this plan

- `user-svc` and `event-svc` — one plan each.
- **Refresh-token rotation.** `refreshAccessToken` returns the same refresh token, so a stolen one stays valid for its whole sliding window with no reuse detection. That is a design change, not a fix.
- The gateway's controllers and guards. They are thin; the filter and the auth service are where the contracts live.
- Fixing `npm run lint` across the four services (ESLint 9 vs `.eslintrc.js`).
- Clearing stale `user_tokens:<id>` sets at deploy. They hold raw tokens written by the old code whose `refresh_token:` keys no longer resolve, and nothing can `srem` them because nothing hashes to them. They age out on `jwtRefreshExpiration`; a `user_tokens:*` flush at deploy closes the window immediately.
