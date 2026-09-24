# Payment Lambdas

The provider-facing half of payment-svc, deployed with SAM from `template.yaml`.

| Function | Trigger | Does |
|---|---|---|
| `payment-webhook-handler` | API Gateway, `POST /webhook/{provider}` | verifies the provider's signature, then completes the payment and writes its outbox row in one transaction |
| `outbox-cleanup` | EventBridge, daily | deletes old published outbox rows; sends rows past their retry limit to an SQS DLQ, which raises a CloudWatch alarm |

`common/` is a shared layer: the Kysely + `pg` database access, the Kafka client, the
logger and the types.

What the functions guarantee, and why the relay that publishes the outbox is a
long-lived worker instead of a Lambda, is in `services/payment-svc/CLAUDE.md`, *The
Lambdas*, and `.claude/skills/outbox-relay/SKILL.md`. On k3s and EKS neither
function runs: `deploy/adapters/payment-events/webhook.js` stands in for the
provider.

## Build and test

```bash
npm ci
npm run build          # tsc
npm test               # jest
npm run build:layers   # the dependencies and common layers SAM deploys
```

## Deploy

```bash
sam build
sam deploy --parameter-overrides VpcId=... PrivateSubnetIds=... DatabaseUrl=... KafkaBrokers=...
```

The parameters and their meaning are in `template.yaml`, *Parameters*; the environment
each function receives is set there too, and the variables the code reads are in
`common/config/index.ts`.
