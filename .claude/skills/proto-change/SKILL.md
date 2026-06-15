---
name: proto-change
description: Use when editing a gRPC contract in proto/ (adding/changing an RPC, message, or field) and the change must be regenerated across the Go and TS services that consume it. Replaces the old, broken `make update-proto`/`update:proto` (which pulled a now-removed git submodule).
---

# Changing a proto contract

There is **one source of truth**: the root `proto/` directory (`user`, `event`, `order`, `payment`, `inventory`, `waitroom`). Generated code is committed, so consumers build without regenerating, but you must regenerate after any contract change.

**Local `.proto` copies:** Go services keep none (compiled stubs only). Each **TS** service keeps a `src/protos/` that is *synced from root* — it's required because the NestJS gRPC transport loads `.proto` at runtime (`protoPath`) and `nest-cli.json` copies it into `dist`. Treat `src/protos/` as generated: never hand-edit it; `npm run update:proto` re-syncs it from `proto/` and regenerates. Do not recreate the old dead `protos/` / `protos-submodule/` dirs.

## Steps

1. **Edit the contract** in `proto/<name>.proto`. Keep field numbers stable; add new fields with new numbers (never renumber/reuse — it breaks the wire format).
2. **Regenerate every consumer** from the repo root:
   ```bash
   make proto          # all consumers (Go + TS)
   # or narrow it:
   make proto-go       # order, inventory, waitroom
   make proto-ts       # api-gateway, event, user, payment
   ```
   TS regen needs the service's `node_modules` — run `npm install` in the service first if `protoc-gen-ts_proto` fails with `MODULE_NOT_FOUND`.
3. **Review the generated diff** (`git diff` in `*/src/protogen`, `*/pkg/grpc`, `waitroom-svc/protogen`) and **commit the stubs together with the `.proto` change** in the same commit.

## Who consumes what (so you know what breaks)

| proto | Go consumers | TS consumers |
|-------|--------------|--------------|
| user | — | api-gateway, user-svc |
| event | order-svc, waitroom-svc | api-gateway, event-svc |
| inventory | order-svc, inventory-svc | api-gateway |
| order | order-svc | api-gateway |
| payment | order-svc | payment-svc |
| waitroom | — | api-gateway |

After regenerating, update the **server implementation** (the service that owns the RPC) and every **client** call site. For a new RPC end-to-end, use the `add-grpc-endpoint` skill.

## Generation mechanics (reference)
- **Go** (`order`, `inventory`, `waitroom`): `make -C services/<svc> protoc-all` → `protoc --go_out/--go-grpc_out ... -I=../../proto` into `pkg/grpc/<svc>/` (waitroom: `protogen/<svc>/`).
- **TS** (`api-gateway`, `event`, `user`, `payment`): `npm run proto:all` → `protoc` + `ts-proto` (`nestJs=true`, `fileSuffix=.pb`) `-I=../../proto` into `src/protogen/`.
- Target state is `buf generate` from a single root module; until then the per-service scripts above all read from root `proto/`.
