# TicketBottle Development Environment

Local orchestration for all TicketBottle V2 services. Compose files and the
Makefile live here; service code lives under `../services/` (one dir per service,
e.g. `order-svc`, `api-gateway`).

## Supported mode: AWS / DynamoDB (LocalStack)

`order-svc` is **DynamoDB-only**, so the supported local stack is the AWS mode,
which runs DynamoDB (and Lambda, etc.) inside LocalStack.

```bash
make up-aws      # build + start everything (DynamoDB via LocalStack)
make down-aws    # stop it
make status      # container status
make logs-aws    # tail all logs   (make logs-order / logs-consumer / logs-localstack for one)
make clean       # stop both modes + remove volumes, then 'docker system prune -f' (system-wide!)
```

> **Legacy MongoDB mode (`make up` / `make down`) is non-functional.** It predates
> the DynamoDB migration; `order-svc` no longer ships a MongoDB driver, so the
> `mongo-order` container has nothing to talk to. It is kept only for history and
> should be removed or deliberately revived. Do not use it.

## Build

On `make up-aws`, **most** services are built from their source directory — the
compose build contexts live under `../services/` (e.g. `../services/event-svc`;
note the API Gateway dir is `../services/api-gateway`, with no `-svc` suffix).
**Order and Payment are the exception:** `docker-compose.aws.yml` runs them from
prebuilt images (`ticketbottle-order-api:aws`, `ticketbottle-order-consumer:aws`,
`ticketbottle-payment-service:aws`) with no `build:` stanza — build those from
their service dirs first, or `make up-aws` will fail because the images don't
exist. There is **no** manual branch checkout step: this is a single monorepo on
`main`, not separate sibling repos.

## Service ports

| Service       | Port            | Protocol            |
|---------------|-----------------|---------------------|
| API Gateway   | 3000            | HTTP/REST + Swagger |
| User          | 50052           | gRPC                |
| Event         | 50053           | gRPC                |
| Order         | 50054           | gRPC                |
| Payment       | 50055 (+ 8085)  | gRPC                |
| Waitroom      | 50056           | gRPC                |
| Inventory     | 50057           | gRPC                |

Infrastructure: Kafka 9092 (UI 8090), Temporal 7233 (UI 8080), Redis 6379
(waitroom) / 6380 (auth), LocalStack 4566. Postgres: Payment 5433, Event 5434,
Inventory 5435, User 5436.

Per-service env files live in `envs/.env.*`.

## How it works

The API Gateway (HTTP) is the only entry point; everything behind it is gRPC. The
Order service drives a Temporal saga (Event → Inventory → Payment) and reacts to
Kafka events. See the root `README.md` and `CLAUDE.md` for the full architecture
and the end-to-end purchase flow.

## Troubleshooting

- **A service image is missing / out of date:** rebuild from its dir, e.g.
  `docker-compose -f docker-compose.aws.yml build order-service`.
- **Order can't reach DynamoDB:** confirm LocalStack is healthy
  (`make logs-localstack`) and that `envs/.env.order.aws` points
  `DYNAMODB_ENDPOINT` at LocalStack.
- **Check DynamoDB table:** `make test-dynamodb`.
- **Shell into LocalStack:** `make shell-localstack`.
