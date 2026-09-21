# user-svc Test Coverage Implementation Plan

**Status: COMPLETE 2026-09-21.** All four tasks done; the checkboxes are a record, not open work.

**Goal:** Put `user-svc` under unit test, and fix the four defects the tests are written to catch — chief among them that this service can never emit the one metric label its alert watches.

**Architecture:** Same shape as the two completed plans: jest with `moduleNameMapper` aliases, `Test.createTestingModule` with every collaborator supplied as a plain mock, no database and no network. Each task writes a failing test first, then the smallest fix that turns it green, then commits.

**Tech Stack:** TypeScript 5.7, NestJS 11, jest 29 + ts-jest 29, `@nestjs/testing` 11, Prisma 6.16, prom-client 15.

**Spec:** none. This plan is derived from a read of `services/user-svc/src/` on 2026-09-21; the defects below were found in that read, and each one is stated with the evidence that makes it real.

**Line numbers throughout are as of that read.** Each task shifts the ones below it — Task 1 alone adds seven lines above `findAll`. Match on the quoted block text, not on the line number.

## Global Constraints

- **ts-jest type-checks every spec.** A type error in one spec fails the whole suite, not one test. Compile what you write.
- **`strictNullChecks` is `false`** in this service, so `null` flows into non-nullable positions without complaint. Do not turn it on here; that is a separate change with a much wider blast radius.
- **Never write `as any`.** `as unknown as ExecutionContext` is allowed for framework context objects that have a dozen methods you do not use — nothing else.
- **Prettier is the only formatting gate that actually runs.** `npm run lint` is broken repo-wide (ESLint 9 against an `.eslintrc.js` it cannot read). Run `npx prettier --check "src/**/*.ts"` before every commit.
- **Error codes in this service are strings**, unlike payment-svc and event-svc: `ErrorCodeEnum.UserNotFound = 'USR000'`, `UserAlreadyExists = 'USR001'`, `PermissionDenied = 'USR403'`.
- **CI pins `node-version: "20"`** — it matches the service Dockerfiles. `@types/node` is 22; the runtime is not.
- **Commit messages describe the platform.** No mention of plans, tasks, or test-writing sessions.

---

## What is wrong today

Four defects, in the order the tasks fix them.

**1. A lost signup race answers `INTERNAL`, which pages.** `UserServiceImpl.create` reads `findUnique({where:{email}})` and then calls `create`. Between those two statements another request can insert the same email — `email` is `@unique` in `prisma/schema.prisma`, so the second insert raises Prisma `P2002`. Nothing catches it. It is not an `RpcException`, so `GlobalGrpcExceptionFilter` takes its `else` branch and the caller gets a codeless failure. Per the root `CLAUDE.md`, a duplicate create is `ALREADY_EXISTS`/409; `INTERNAL` means *we have a bug* and is the one code the alerting policy pages on. A double-clicked signup button is enough to trigger it. **Live.**

**2. The service cannot emit `INTERNAL` at all.** `grpc-metrics.interceptor.ts:14-19` labels anything that is not an `RpcException` carrying a numeric `code` as `code="UNKNOWN"`. The interceptor sits outside the handler and therefore sees the *original* exception, before the filter rewrites it — a Prisma error, a `TypeError`, anything genuinely our fault. So every real bug in `user-svc` is counted as `UNKNOWN`. The alerting policy pages on `code="INTERNAL"` and there is no `UNKNOWN` row in the taxonomy at all, so **no bug in this service can ever page.** The interceptor's own comment describes this behaviour as intended; it is not — it is the same metric/HTTP divergence already fixed in the gateway. **Live.**

The filter is the other half: `throwError(() => new RpcException('Internal server error'))` puts an `RpcException` *instance* on the wire instead of an error payload, so grpc-js sends `UNKNOWN(2)` rather than `INTERNAL(13)`. The wire code and the metric label are wrong in the same direction, which is why neither one contradicts the other.

**3. `findAll` has a `catch` that can never run, and returns `undefined` if it ever did.** `user.service.ts:57-70` wraps `return this.prisma.user.findMany(...)` in `try` without `await`. The promise is returned, so a rejection propagates to the caller and the `catch` never sees it — the block is dead code. If it ever did run it would `console.log` and fall off the end returning `undefined`, and `user.controller.ts:65` immediately calls `users.map(...)` on it. **Dead code; the `undefined` path is unreachable today.**

Separately, a query with no filters builds `OR: [{id:{in:undefined}}, ...]`. Prisma drops `undefined`, leaving an `OR` of empty conditions, which selects every row — and `parseUserToPb` spreads the whole Prisma record, password hash included. **Latent:** the gateway exposes no `findAll` route, so nothing reaches this path today.

**4. `services/user-svc/CLAUDE.md` says passwords are hashed with bcrypt.** There is no bcrypt in this service and no hashing of any kind. The gateway hashes with argon2 (`auth.service.ts:40`) and sends the digest; `user-svc` stores what it is given. A reader looking for the hashing here will not find it.

## File structure

| File | Change | Task |
|---|---|---|
| `services/user-svc/package.json` | jest `moduleNameMapper` | 1 |
| `services/user-svc/src/user/user.service.spec.ts` | create | 1, 3 |
| `services/user-svc/src/user/user.service.ts` | P2002 catch; `findAll` rewrite | 1, 3 |
| `services/user-svc/src/common/interceptors/grpc-metrics.interceptor.spec.ts` | create | 2 |
| `services/user-svc/src/common/interceptors/grpc-metrics.interceptor.ts` | `codeOf` returns INTERNAL | 2 |
| `services/user-svc/src/common/filters/global-grpc-exception.filter.spec.ts` | create | 2 |
| `services/user-svc/src/common/filters/global-grpc-exception.filter.ts` | wire code; drop `console.log` | 2 |
| `.github/workflows/ts-tests.yml` | `user-svc` job + paths | 4 |
| `services/user-svc/CLAUDE.md` | correct the hashing claim | 4 |
| `services/user-svc/src/shared/constants/error-code.constant.ts` | Prettier | 4 |

---

### Task 1: Jest aliases, and the signup race

**Files:**
- Modify: `services/user-svc/package.json` (the `jest` block)
- Modify: `services/user-svc/src/user/user.service.ts:15-26`
- Test: `services/user-svc/src/user/user.service.spec.ts` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `services/user-svc/src/user/user.service.spec.ts` with two exported-by-convention helpers used again in Task 3 — `buildService(prisma: any): Promise<UserServiceImpl>` and `errorOf(e: unknown): { code?: number; message?: string }`. Both are module-local `const`/`function` declarations in that file, not exports.

- [ ] **Step 1: Add the module aliases jest needs**

Only three aliases appear in `src/`: `@/`, `@services/`, `@interfaces/`. `tsconfig.json` declares ten more that point at directories this service does not have — do not copy those in.

In `services/user-svc/package.json`, inside the `"jest"` object, add `moduleNameMapper` immediately after `"rootDir": "src",`:

```json
    "rootDir": "src",
    "moduleNameMapper": {
      "^@/(.*)$": "<rootDir>/$1",
      "^@services/(.*)$": "<rootDir>/shared/services/$1",
      "^@interfaces/(.*)$": "<rootDir>/shared/interfaces/$1"
    },
```

- [ ] **Step 2: Write the failing test**

Create `services/user-svc/src/user/user.service.spec.ts`:

```ts
import { status as grpcStatus } from '@grpc/grpc-js';
import { RpcException } from '@nestjs/microservices';
import { Test } from '@nestjs/testing';
import { PrismaService } from '@/shared/prisma/prisma.service';
import { CreateUserDto } from './dto/create-user.dto';
import { UserServiceImpl } from './user.service';

const signup: CreateUserDto = {
  email: 'buyer@example.com',
  firstName: 'Buyer',
  lastName: 'One',
  password: '$argon2id$v=19$m=65536,t=3,p=4$hash',
};

async function buildService(prisma: any): Promise<UserServiceImpl> {
  const moduleRef = await Test.createTestingModule({
    providers: [UserServiceImpl, { provide: PrismaService, useValue: prisma }],
  }).compile();
  return moduleRef.get(UserServiceImpl);
}

// RpcException.getError() is typed `string | object`; every exception this
// service means to raise carries the object form. Anything else escaped
// unclassified, which is the defect these cases are about -- so report it as a
// missing code rather than crashing on the missing method.
const errorOf = (e: unknown): { code?: number; message?: string } =>
  e instanceof RpcException ? (e.getError() as { code?: number; message?: string }) : {};

const rejectionOf = (p: Promise<unknown>): Promise<unknown> =>
  p.then(
    () => null,
    (e) => e,
  );

describe('UserServiceImpl.create', () => {
  it('refuses an email the pre-check already sees', async () => {
    const service = await buildService({
      user: { findUnique: jest.fn().mockResolvedValue({ id: 'u-1' }), create: jest.fn() },
    });

    const error = await rejectionOf(service.create(signup));

    expect(errorOf(error).code).toBe(grpcStatus.ALREADY_EXISTS);
  });

  it('answers a lost signup race ALREADY_EXISTS, not a code that pages', async () => {
    const prisma = {
      user: {
        findUnique: jest.fn().mockResolvedValue(null),
        create: jest
          .fn()
          .mockRejectedValue(Object.assign(new Error('unique'), { code: 'P2002' })),
      },
    };
    const service = await buildService(prisma);

    const error = await rejectionOf(service.create(signup));

    expect(errorOf(error).code).toBe(grpcStatus.ALREADY_EXISTS);
  });

  it('lets a failure that is not a duplicate through untouched', async () => {
    const service = await buildService({
      user: {
        findUnique: jest.fn().mockResolvedValue(null),
        create: jest.fn().mockRejectedValue(new Error('connection refused')),
      },
    });

    await expect(service.create(signup)).rejects.toThrow('connection refused');
  });
});
```

- [ ] **Step 3: Run the tests and watch the second one fail**

Run: `cd services/user-svc && npm test -- user.service`

Expected: the first and third pass; **the second fails** with `Expected: 6 / Received: undefined` — the P2002 error escapes `create` unchanged, carrying no gRPC code at all.

If the second one *passes* here, stop: the fix is already in and the test proves nothing. Re-read `user.service.ts` before going on.

- [ ] **Step 4: Catch the constraint the pre-check cannot see**

In `services/user-svc/src/user/user.service.ts`, replace the body of `create` (lines 15-26):

```ts
  async create(dto: CreateUserDto): Promise<User> {
    const user = await this.prisma.user.findUnique({
      where: { email: dto.email },
    });
    if (user) {
      throw new RpcBusinessException(ErrorCodeEnum.UserAlreadyExists);
    }

    try {
      // `return await`, not `return`: an un-awaited promise settles outside this
      // try block and the catch never runs.
      return await this.prisma.user.create({ data: dto });
    } catch (error) {
      // P2002 = the unique email was taken between the read above and this
      // insert. Only the constraint can see that race.
      if ((error as { code?: string }).code !== 'P2002') throw error;
      throw new RpcBusinessException(ErrorCodeEnum.UserAlreadyExists);
    }
  }
```

- [ ] **Step 5: Run the tests and the formatter**

Run: `cd services/user-svc && npm test -- user.service && npx prettier --check "src/**/*.ts"`

Expected: 3 passed. Prettier clean — if it flags `error-code.constant.ts`, leave it; Task 4 owns that file.

- [ ] **Step 6: Commit**

```bash
git add services/user-svc/package.json services/user-svc/src/user/user.service.ts services/user-svc/src/user/user.service.spec.ts
git commit -m "$(cat <<'EOF'
fix(user): a lost signup race is a duplicate, not a server fault

The email pre-check cannot see a concurrent insert; the unique constraint
can. Catching P2002 keeps a double-clicked signup out of the INTERNAL
bucket, which pages.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Make the code that pages reachable

The alerting policy pages on `code="INTERNAL"`. This service labels every unexpected failure `UNKNOWN`, so no bug here can page. Fix both halves — the metric label and the wire code — so they agree.

**Files:**
- Modify: `services/user-svc/src/common/interceptors/grpc-metrics.interceptor.ts:8-19`
- Modify: `services/user-svc/src/common/filters/global-grpc-exception.filter.ts`
- Test: `services/user-svc/src/common/interceptors/grpc-metrics.interceptor.spec.ts` (create)
- Test: `services/user-svc/src/common/filters/global-grpc-exception.filter.spec.ts` (create)

**Interfaces:**
- Consumes: the jest aliases from Task 1.
- Produces: nothing later tasks import.

- [ ] **Step 1: Write the failing interceptor test**

`grpcRequests` is a module-level prom-client counter, so it is shared state across tests in the file — reset it in `beforeEach` or the second case reads the first one's labels.

Create `services/user-svc/src/common/interceptors/grpc-metrics.interceptor.spec.ts`:

```ts
import { status as grpcStatus } from '@grpc/grpc-js';
import { ExecutionContext, CallHandler } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { Observable, of, throwError } from 'rxjs';
import { grpcRequests } from '@/shared/metrics/metrics';
import { GrpcMetricsInterceptor } from './grpc-metrics.interceptor';

const context = {
  getArgByIndex: () => ({ getPath: () => '/user.UserService/Create' }),
  getClass: () => ({ name: 'UserController' }),
  getHandler: () => ({ name: 'create' }),
} as unknown as ExecutionContext;

const handlerOf = (result: Observable<unknown>): CallHandler => ({ handle: () => result });

// The label under which this call was counted, which is the only thing an
// alert rule can see.
async function codeLabelAfter(result: Observable<unknown>): Promise<string> {
  const interceptor = new GrpcMetricsInterceptor();
  await new Promise<void>((resolve) => {
    interceptor.intercept(context, handlerOf(result)).subscribe({
      next: () => undefined,
      error: () => resolve(),
      complete: () => resolve(),
    });
  });

  const counted = await grpcRequests.get();
  return String(counted.values[0].labels.code);
}

describe('GrpcMetricsInterceptor', () => {
  beforeEach(() => grpcRequests.reset());

  it('counts a success as OK', async () => {
    expect(await codeLabelAfter(of({ user: null }))).toBe('OK');
  });

  it('counts a business rejection under its own code', async () => {
    const rejection = new RpcException({
      code: grpcStatus.ALREADY_EXISTS,
      message: 'USR001 - User already exists',
    });

    expect(await codeLabelAfter(throwError(() => rejection))).toBe('ALREADY_EXISTS');
  });

  it('counts an unclassified failure as INTERNAL, the code the alert watches', async () => {
    expect(await codeLabelAfter(throwError(() => new Error('prisma exploded')))).toBe('INTERNAL');
  });
});
```

- [ ] **Step 2: Run it and watch the third case fail**

Run: `cd services/user-svc && npm test -- grpc-metrics`

Expected: the first two pass; **the third fails** with `Expected: "INTERNAL" / Received: "UNKNOWN"`.

- [ ] **Step 3: Label an unclassified failure for what it is**

In `services/user-svc/src/common/interceptors/grpc-metrics.interceptor.ts`, replace the comment block and `codeOf` (lines 8-19):

```ts
// The code the CALLER sees, not the class of the exception thrown here:
//
// RpcBusinessException | RpcValidationException -> { code } from the ErrorCode
//                                                  tuple -> that code on the wire
// anything else -> a bug, which the taxonomy calls INTERNAL. UNKNOWN is in no
//                  row of that table and in no alert rule, so it would hide one.
const codeOf = (err: unknown): string => {
  const error = err instanceof RpcException ? err.getError() : undefined;
  const code =
    typeof error === 'object' && error !== null ? (error as { code?: unknown }).code : undefined;
  if (typeof code !== 'number') return 'INTERNAL';
  return grpcStatus[code] ?? 'INTERNAL';
};
```

- [ ] **Step 4: Run the interceptor tests**

Run: `cd services/user-svc && npm test -- grpc-metrics`

Expected: 3 passed.

- [ ] **Step 5: Write the failing filter test**

Create `services/user-svc/src/common/filters/global-grpc-exception.filter.spec.ts`:

```ts
import { status as grpcStatus } from '@grpc/grpc-js';
import { ArgumentsHost } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { LoggerService } from '@/shared/services/logger.service';
import { GlobalGrpcExceptionFilter } from './global-grpc-exception.filter';

const host = {} as ArgumentsHost;

const loggerStub = {
  setContext: jest.fn(),
  error: jest.fn(),
} as unknown as LoggerService;

// What the filter puts on the wire, which is what grpc-js turns into a status.
async function wireError(exception: unknown): Promise<{ code?: number; message?: string }> {
  const filter = new GlobalGrpcExceptionFilter(loggerStub);
  return new Promise((resolve) => {
    filter.catch(exception, host).subscribe({
      error: (e) => resolve(e as { code?: number; message?: string }),
    });
  });
}

describe('GlobalGrpcExceptionFilter', () => {
  it('passes a business rejection through with its own code', async () => {
    const sent = await wireError(
      new RpcException({ code: grpcStatus.NOT_FOUND, message: 'USR000 - User not found' }),
    );

    expect(sent.code).toBe(grpcStatus.NOT_FOUND);
    expect(sent.message).toBe('USR000 - User not found');
  });

  it('sends an unexpected failure as INTERNAL, not as a codeless error', async () => {
    const sent = await wireError(new Error('prisma exploded'));

    expect(sent.code).toBe(grpcStatus.INTERNAL);
    expect(sent.message).toBe('Internal server error');
  });

  it('does not leak the underlying message to the caller', async () => {
    const sent = await wireError(new Error('connect ECONNREFUSED 10.0.1.4:5432'));

    expect(sent.message).not.toContain('10.0.1.4');
  });
});
```

- [ ] **Step 6: Run it and watch the second case fail**

Run: `cd services/user-svc && npm test -- global-grpc-exception`

Expected: the first and third pass; **the second fails** — the filter emits an `RpcException` instance, so `sent.code` is `undefined` rather than `13`.

- [ ] **Step 7: Put a real code on the wire**

Replace `services/user-svc/src/common/filters/global-grpc-exception.filter.ts` in full:

```ts
import { LoggerService } from '@/shared/services/logger.service';
import { status as grpcStatus } from '@grpc/grpc-js';
import { ArgumentsHost, Catch, RpcExceptionFilter } from '@nestjs/common';
import { RpcException } from '@nestjs/microservices';
import { Observable, throwError } from 'rxjs';

@Catch()
export class GlobalGrpcExceptionFilter implements RpcExceptionFilter<any> {
  constructor(private readonly logger: LoggerService) {
    this.logger.setContext(GlobalGrpcExceptionFilter.name);
  }

  catch(exception: any, host: ArgumentsHost): Observable<any> {
    if (exception instanceof RpcException) {
      return throwError(() => exception.getError());
    }

    this.logger.error(`Unhandled exception: ${exception?.message}`, exception?.stack);

    // A payload, not an RpcException instance: grpc-js reads `code` off the
    // error it is given, and an instance without one goes out as UNKNOWN.
    return throwError(() => ({
      code: grpcStatus.INTERNAL,
      message: 'Internal server error',
    }));
  }
}
```

The two `console.log` calls are gone with it — the service has a structured logger, and the one in the `if` branch logged every expected business outcome at full volume.

- [ ] **Step 8: Run the whole suite and the formatter**

Run: `cd services/user-svc && npm test && npx prettier --check "src/**/*.ts"`

Expected: 9 passed across three files.

- [ ] **Step 9: Commit**

```bash
git add services/user-svc/src/common/interceptors/grpc-metrics.interceptor.ts services/user-svc/src/common/interceptors/grpc-metrics.interceptor.spec.ts services/user-svc/src/common/filters/global-grpc-exception.filter.ts services/user-svc/src/common/filters/global-grpc-exception.filter.spec.ts
git commit -m "$(cat <<'EOF'
fix(user): a bug here is INTERNAL, the code the alert watches

The interceptor labelled every unclassified failure UNKNOWN and the filter
sent one, so no defect in this service could reach the page rule. Both
halves now say what the taxonomy says.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: findAll selects somebody, or nobody

**Files:**
- Modify: `services/user-svc/src/user/user.service.ts:56-71`
- Test: `services/user-svc/src/user/user.service.spec.ts` (append a second `describe`)

**Interfaces:**
- Consumes: `buildService` from Task 1, already in this file.
- Produces: nothing.

- [ ] **Step 1: Write the failing tests**

Append to `services/user-svc/src/user/user.service.spec.ts`. Add `QueryUserDto` to the imports at the top of the file:

```ts
import { QueryUserDto } from './dto/query-user.dto';
```

Then, after the existing `describe`:

```ts
describe('UserServiceImpl.findAll', () => {
  const query = (over: Partial<QueryUserDto>): QueryUserDto => ({
    ids: undefined,
    emails: undefined,
    name: undefined,
    ...over,
  });

  it('selects nobody when nothing was asked for', async () => {
    const findMany = jest.fn();
    const service = await buildService({ user: { findMany } });

    expect(await service.findAll(query({}))).toEqual([]);
    expect(findMany).not.toHaveBeenCalled();
  });

  it('builds a clause only for the filters it was given', async () => {
    const findMany = jest.fn().mockResolvedValue([]);
    const service = await buildService({ user: { findMany } });

    await service.findAll(query({ ids: ['u-1'], emails: [] }));

    expect(findMany).toHaveBeenCalledWith({ where: { OR: [{ id: { in: ['u-1'] } }] } });
  });

  it('searches both names for one term', async () => {
    const findMany = jest.fn().mockResolvedValue([]);
    const service = await buildService({ user: { findMany } });

    await service.findAll(query({ name: 'Buyer' }));

    expect(findMany).toHaveBeenCalledWith({
      where: {
        OR: [{ firstName: { contains: 'Buyer' } }, { lastName: { contains: 'Buyer' } }],
      },
    });
  });

  it('lets a query failure reach the caller instead of returning undefined', async () => {
    const service = await buildService({
      user: { findMany: jest.fn().mockRejectedValue(new Error('db down')) },
    });

    await expect(service.findAll(query({ name: 'Buyer' }))).rejects.toThrow('db down');
  });
});
```

- [ ] **Step 2: Run them and watch two fail**

Run: `cd services/user-svc && npm test -- user.service`

Expected: the first three fail. The first because today's code always calls `findMany` and returns its result; the second and third because today's `OR` carries all four clauses regardless of what was asked for. The fourth passes already — the `catch` is dead, so the rejection propagates. It stays green after the rewrite, then for a reason rather than by accident.

- [ ] **Step 3: Build the filter list from what was actually asked**

In `services/user-svc/src/user/user.service.ts`, replace `findAll` (lines 56-71):

```ts
  async findAll(dto: QueryUserDto): Promise<User[]> {
    const filters: Prisma.UserWhereInput[] = [];
    if (dto.ids?.length) filters.push({ id: { in: dto.ids } });
    if (dto.emails?.length) filters.push({ email: { in: dto.emails } });
    if (dto.name) {
      filters.push({ firstName: { contains: dto.name } });
      filters.push({ lastName: { contains: dto.name } });
    }

    // Prisma drops an undefined filter, so an unfiltered query would leave an OR
    // of empty conditions -- every user, password hash and all.
    if (filters.length === 0) return [];

    return this.prisma.user.findMany({ where: { OR: filters } });
  }
```

Change the Prisma import on line 5 to bring in the namespace:

```ts
import { Prisma, User } from '@prisma/client';
```

The `try`/`catch` is gone. It never ran — `return` without `await` settles the promise outside the block — and its only effect if it had was to `console.log` and return `undefined` into `users.map(...)` at `user.controller.ts:65`.

- [ ] **Step 4: Run the suite**

Run: `cd services/user-svc && npm test && npx prettier --check "src/**/*.ts"`

Expected: 13 passed.

Then mutate to check the tests bite: change `if (filters.length === 0) return [];` to `return null;` and confirm the first case fails. Put it back.

- [ ] **Step 5: Commit**

```bash
git add services/user-svc/src/user/user.service.ts services/user-svc/src/user/user.service.spec.ts
git commit -m "$(cat <<'EOF'
fix(user): an unfiltered search selects nobody, not everybody

Prisma drops undefined filters, so a query with none left an OR of empty
conditions matching every row. The dead try/catch went with it: an
un-awaited return never reaches its own catch.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: Run it in CI, and correct the record

**Files:**
- Modify: `.github/workflows/ts-tests.yml`
- Modify: `services/user-svc/CLAUDE.md`
- Modify: `services/user-svc/src/shared/constants/error-code.constant.ts`

**Interfaces:**
- Consumes: a green `npm test` in `services/user-svc`.
- Produces: nothing.

- [ ] **Step 1: Add user-svc to the workflow**

`user-svc` needs `npx prisma generate` — no TS service has a `postinstall`, and `node_modules/` is gitignored, so CI installs a client that has not been generated. Without it `jest` cannot even *resolve* `@prisma/client/default` at require time, so the suite dies before types matter; `tsc` then adds 14 errors of its own. `payment-svc` has the same step. `api-gateway` does not — and must not: it has no `prisma/schema.prisma` at all, so the command would fail there for want of a schema.

In `.github/workflows/ts-tests.yml`, add `"services/user-svc/**"` to both `paths` lists (the `push` one keeps `.github/workflows/ts-tests.yml` as its last entry), then append this job after `api-gateway`:

```yaml
  user-svc:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: services/user-svc
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          # Matches the service Dockerfiles; @types/node is 22, the runtime is not.
          node-version: "20"

      - run: npm ci

      - name: Generate the Prisma client
        run: npx prisma generate

      - run: npm test
```

- [ ] **Step 2: Correct the hashing claim**

In `services/user-svc/CLAUDE.md`, under `## Notes`, replace the first bullet:

```markdown
- Passwords arrive already hashed — the API Gateway hashes with argon2 and this service stores the digest verbatim. It neither hashes nor verifies; it is the store of record for credentials and for the JWT subject claims read elsewhere.
```

- [ ] **Step 3: Let Prettier collapse the frozen table**

Run: `cd services/user-svc && npx prettier --write src/shared/constants/error-code.constant.ts`

Expected: the `Object.freeze<...>` generic argument collapses onto one line — it is 94 characters, inside the 100 printWidth. Nothing else in the file changes.

- [ ] **Step 4: Verify the whole suite one last time**

Run: `cd services/user-svc && npm test && npx prettier --check "src/**/*.ts"`

Expected: 13 passed, and `[warn] Code style issues found in 3 files` — `src/shared/services/config.service.ts` and the two `src/protogen/*.pb.ts`, all dirty before this plan and all out of scope. `error-code.constant.ts` is no longer among them.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ts-tests.yml services/user-svc/CLAUDE.md services/user-svc/src/shared/constants/error-code.constant.ts
git commit -m "$(cat <<'EOF'
ci: run the user service's unit tests

Its Prisma client is generated at test time, as the payment service's is.
Also corrects the service's note on password hashing: the gateway hashes
with argon2, this service stores the digest.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

## Found, not fixed

- **A stale `tsbuildinfo` makes `tsc --noEmit` lie in this service.** `tsconfig.json` sets `incremental: true`, so with the Prisma client deleted a plain `npx tsc --noEmit` still reported zero errors while `npx tsc --noEmit --incremental false` reported 14. Any type-check used as evidence here needs `--incremental false`.

Recorded here so the next reader does not rediscover them. None is in scope for this plan.

- **`rpc Delete` has no handler.** `proto/user.proto:11` declares it; `user.controller.ts` implements `create`, `update`, `findOne`, `findAll` and nothing else. A call returns `UNIMPLEMENTED(12)`, which the gateway maps to 501 — and 501 pages nobody, so a deploy-skew gap here is silent. Nothing calls it today.
- **Every response carries the password hash.** `parseUserToPb` spreads the whole Prisma row, so `password` is on the wire for `findOne`, `create`, `update` and `findAll`. The gateway strips it with `@Exclude()` on `UserDto`, so it does not reach an HTTP client — but the gateway *needs* it, because `argon2.verify` runs there rather than here. Moving verification into `user-svc` would take the hash off the wire entirely. That is a contract change across two services.
- **`findOne` can issue two queries.** `user.controller.ts:48-54` tests `dto.email` and `dto.id` in sequence, and the id result overwrites the email one. `FindOneUserRequest` is a `oneof`, so only one arrives today.
- **Name search is case-sensitive.** `contains` without `mode: 'insensitive'` means "buyer" does not match "Buyer". Left alone because changing it changes which index the query can use.
- **`AppController` / `AppService` are Nest scaffolding.** An HTTP controller in a service that boots `createMicroservice` and serves no HTTP. Dead, and registered in `AppModule`.
- **`update` has the same read-then-write race as `create` had.** Unreachable while nothing deletes users, and it would raise `P2025` rather than `P2002`.

## Self-review

- **Coverage.** Four defects named in "What is wrong today", four tasks, one each. Task 4 also carries CI, which nothing else needed.
- **Placeholders.** None: every step has the code or the exact command.
- **Type consistency.** `buildService` and `errorOf` are declared in Task 1 and reused in Task 3 from the same file. `Prisma.UserWhereInput` requires the namespace import added in Task 3 Step 3. `grpcRequests.get()` resolves to `{ values: [{ value, labels }] }` — verified against prom-client 15 in this service's `node_modules`, not assumed.
- **Test-count arithmetic.** 3 (Task 1) + 3 + 3 (Task 2) + 4 (Task 3) = 13. Step 4 of Task 3 and Step 4 of Task 4 both expect 13.
