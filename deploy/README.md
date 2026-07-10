# TicketBottle Deployment (Rung 1: local kind)

Portable Helm chart for the TicketBottle stack. Rung 1 runs everything on a local
`kind` cluster for $0. See the spec:
`docs/superpowers/specs/2026-07-09-aws-affordable-deployment-ladder-design.md`.

## Prerequisites
docker (daemon running), kubectl, helm, kind.

## Bring up the infra tier (Phase 0A)
```bash
cd deploy
make cluster-up     # one-time: create the kind cluster
make infra-up       # install Postgres, Redis, Redpanda, DynamoDB-local, Temporal
make smoke          # verify every component
```

## Tear down
```bash
make infra-down     # remove the chart, keep the cluster
make cluster-down   # delete the cluster entirely
```

## What runs (infra tier)
| Component | Service:port | Notes |
|-----------|--------------|-------|
| Postgres | postgres:5432 | one instance, 4 app DBs + Temporal's 2 |
| Redis | redis:6379 | waitroom (DB0) + gateway auth (DB1) |
| Redpanda | redpanda:9093 | Kafka-API broker (replaces Kafka+ZK) |
| DynamoDB-local | dynamodb:8000 | orders table (PK/SK + GSI1 + GSI2) |
| Temporal | temporal:7233 | Postgres visibility, no Elasticsearch |

App services (8) + the two payment lambda-replacements land in Phase 0B.
