# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

This is **not a service** — it is the shared gRPC **contract source** for TicketBottle V2. For the system-wide picture see the umbrella `../CLAUDE.md`.

## What's here

One `.proto` per service, defining the gRPC API each service exposes and that the API Gateway / other services call:

```
event.proto  inventory.proto  order.proto  payment.proto  user.proto  waitroom.proto
```

It is its own git repository (consumed by the services, historically as a git submodule).

## The golden rule: edit here, regenerate everywhere

Service code is **generated** from these files — never hand-edit generated stubs in a consumer. After changing a `.proto`:

- **Go consumers** (`order`, `inventory`, `waitroom`) vendor these into a `protos-submodule/` directory and regenerate with `make protoc` / `make update-proto`. Generated code lands in `pkg/grpc/<svc>` or `protogen/`.
- **TS consumers** (`api-gateway`, `event`, `user`, `payment`) copy the `.proto` into their `src/protos` (or `protos/`) and run the `proto:*` / `proto:all` npm scripts (ts-proto, `nestJs=true`). Generated code lands in `src/protogen/`.

Because consumption is a mix of copies and submodule checkouts, a contract change is not complete until every consumer has pulled the new `.proto` and regenerated. Keep field numbers backward-compatible (add, don't renumber/reuse) so old and new builds interoperate during rollout.
