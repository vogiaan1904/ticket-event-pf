# Local AWS Simulation (kind + host LocalStack) — retired

> **Status: retired.** Superseded by deploying against real DynamoDB on the AWS
> targets. Kept for reference only; do not build new work on this path.

Runs the TicketBottle purchase flow on the **existing kind cluster** with only the
**AWS-native pieces** moved to a host-side **LocalStack Pro**: order's datastore →
**LocalStack DynamoDB**, and the payment async path → the real **Lambdas**
(`payment-webhook-handler` via API Gateway, `outbox-processor` via EventBridge/manual
invoke) instead of the in-cluster payment-webhook adapter.

Delivered as a `values-localstack.yaml` overlay on the same Helm chart — no infra is
duplicated, kind keeps running everything else.

## Architecture — 3 cross-boundary hops

LocalStack runs as a **host container on the `kind` docker network** (static IP
`172.18.0.100`); the Lambdas it spawns join that network (`LAMBDA_DOCKER_NETWORK=kind`)
and reach kind via the node `ticketbottle-control-plane`.

1. **order-svc (pod) → LocalStack DynamoDB** — via the k8s `localstack` Service/Endpoints → `172.18.0.100:4566`.
2. **payment-webhook-handler Lambda → payment Postgres** — via NodePort `31432`.
3. **outbox-processor Lambda → Redpanda** — via Redpanda's EXTERNAL listener on NodePort `31094`.

Internal clients (order-consumer, payment-svc) keep using the internal listener `redpanda:9093`.

## Prerequisites

- **kind cluster up with the Gate-1 baseline green** (plain kind mode):
  `make -C ../.. ... ` → `make -C ../../deploy cluster-up infra-up apps-up gate1`.
- **Docker Desktop ≥ 8 GB memory** and **≥ ~10 GB free disk** in the VM (the polyglot
  build + kind loads are disk-hungry; prune buildx cache / abandoned images if tight).
- `awslocal` (`pip install awscli-local`) and `jq` on the host.
- **LocalStack Pro auth token** in `deploy/localstack/.env` as `LOCALSTACK_AUTH_TOKEN` (git-ignored).

## Run it

```bash
cd deploy/localstack
make all        # up -> runtime-image -> lambdas -> overlay -> order-restart -> gate
```

…or step by step:

```bash
make up            # start host LocalStack (Pro) on the kind net (creates the orders table via init hook)
make runtime-image # REQUIRED: pull the AMD64 Lambda runtime (see gotcha below)
make lambdas       # deploy the 3 Lambdas + API Gateway (ticketbottle-webhooks)
make overlay       # helm upgrade with values-localstack (dynamodb-local + adapter OFF; bridge + NodePorts ON)
make order-restart # order pods must restart to pick up the LocalStack DynamoDB endpoint
make gate          # Gate 1.5: full purchase flow -> "GATE 1.5 PASSED"
```

Teardown:

```bash
make down          # stop LocalStack (kind untouched)
make revert        # back to plain kind mode (dynamodb-local + adapter) + restart order
```

## Gotchas learned the hard way

- **Static IP assumes the `kind` network is `172.18.0.0/16`.** `docker-compose.yml` pins
  `172.18.0.100` and `values-localstack.yaml`'s `localstack.ip` must match it. On a machine
  where Docker gave the `kind` network a different subnet, run
  `docker network inspect kind -f '{{(index .IPAM.Config 0).Subnet}}'`, pick a free host
  address in that subnet, and update the IP in **both** files.
- **Lambda runtime must be AMD64.** The functions default to `x86_64`, and the Prisma
  engine baked into the layer is `rhel-openssl-3.0.x` (amd64). So LocalStack needs the
  **amd64** runtime image (`make runtime-image`), which runs *emulated* on Apple Silicon
  (slower cold starts, works). Without it: `platform (linux/arm64/v8) does not match
  (linux/amd64)` → API Gateway 500. Do **not** recreate the functions as arm64 — that
  would mismatch the amd64 Prisma engine.
- **Region split.** The DynamoDB table lives in **us-east-1** (order-svc + the init hook).
  Host `awslocal` may default to another region (e.g. `ap-southeast-1`) — add
  `--region us-east-1` for DynamoDB checks. Lambda + API Gateway use the awslocal default
  consistently (the gate + deploy script agree), which is harmless (Lambdas don't touch DynamoDB).
- **order pods don't auto-restart on the ConfigMap change** — `make order-restart` (or
  `make all`) handles it; skipping it leaves order-svc on the old `dynamodb:8000` endpoint.
- **The ZaloPay webhook is signed:** `mac = HMAC_SHA256(<data JSON>, ZALOPAY_KEY2)` (from
  `services/payment-svc/lambdas/envs/env.localstack.json`), `type=1`,
  `app_trans_id = "<YYMMDD>_<ORDER_CODE>"` (order codes have no `_`). `gate.sh` mints it.

## Logs / debugging

```bash
curl -s http://localhost:4566/_localstack/info | jq -r .edition          # expect: pro
awslocal lambda list-functions --query 'Functions[].FunctionName'         # 3 functions (default region)
awslocal dynamodb scan --table-name ticketbottle-orders --region us-east-1 --select COUNT
awslocal logs tail /aws/lambda/payment-webhook-handler --format short
awslocal logs tail /aws/lambda/outbox-processor --format short
```
