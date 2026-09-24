# Gateway source conventions

Loaded whenever gateway source is open. The service's role and commands are in
`../CLAUDE.md`.

**Layout** is owned by `docs/design/ts-layout.md`. A new module uses one `dto/`
(`*.dto.ts` requests, `*.resp.dto.ts` responses). The existing `dtos/req` +
`dtos/resp` modules stay as they are until the gateway is converged as a whole; do
not reshape one module on the side.

## A feature module

- **Client.** Each module registers its own gRPC client with
  `ClientsModule.registerAsync` in `<feature>.module.ts`: the address from
  `AppConfigService.microservicesConfig`, and `protoPath`
  `join(__dirname, '../../protos/<svc>.proto')` — the runtime copy `nest-cli.json`
  ships into `dist`.
- **Service.** Resolves the client in `onModuleInit` with
  `getService<…ServiceClient>(…_SERVICE_NAME)`, wraps every call in
  `firstValueFrom`, and returns proto types.
- **Controller.** Thin: calls the service, then maps proto to the response DTO with a
  static `<Resource>Mapper.toDto`. It never calls gRPC itself.
- **Enums.** The REST contract uses string enums from the module's `enums/`,
  translated by `mappers/`; a proto's numeric enum never reaches a response.
- **Validation.** Request DTOs carry class-validator decorators. `ValidationPipe`
  runs globally with `transform: true`, so a numeric query field needs
  `@Type(() => Number)`.

## Errors

- A **downstream gRPC error propagates untouched.** `GlobalExceptionFilter` maps its
  code through `GRPC_TO_HTTP` in `src/shared/metrics/code.ts`; catching it and
  throwing something else changes both the status and the metric's `code`.
- The **gateway's own refusals** are a `BusinessException` with an `ErrorCodeEnum`
  entry (`src/shared/constants/error-code.constant.ts`), or a guard's
  `HttpException`. Its status needs a row in `HTTP_TO_GRPC`, in the same file, or the
  request is labelled `INTERNAL` — which pages.

## Auth

- A protected route has `@UseGuards(AccessGuard)`; the handler takes
  `@Req() req: RequestWithUser` and passes `req.user` to the service.
- Sign-in and sign-up also carry `AuthRateLimitGuard`: each runs an argon2 hash in
  this process ([0013](../../../docs/decisions/0013-the-gateway-throttles-sign-in-not-purchase.md)).
