# TicketBottle Deployment

Portable Helm chart for the TicketBottle stack. One chart deploys to every target;
the target is selected by a `values-*.yaml` overlay, never by forking a template.

| Target | Overlay | Images | Orders store | Ingress |
|--------|---------|--------|--------------|---------|
| Local `kind` | `values.yaml` | built locally | DynamoDB-local | NodePort 30000 |
| k3s on EC2 | `values-k3s.yaml` | ECR | DynamoDB | NodePort |
| Amazon EKS | `values-eks.yaml` | ECR | DynamoDB | ALB |

Infrastructure for the AWS targets lives in `deploy/terraform/`.

## Prerequisites
docker (daemon running), kubectl, helm, kind.

## Bring up the local stack
```bash
cd deploy
make cluster-up     # one-time: create the kind cluster
make infra-up       # install Postgres, Redis, Redpanda, DynamoDB-local, Temporal
make apps-up        # build + load the app images, deploy the app tier
make gate1          # end-to-end purchase-flow acceptance test
```

`apps-up` is `apps-build` (10 images, ~10 min) then `apps-deploy` (`helm upgrade`). When you have
changed only the chart — templates, values — run `apps-deploy` on its own; the images are already in
the kind node and `pullPolicy` is `IfNotPresent`.

```bash
make apps-deploy    # chart-only: skip the image build
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

The app tier adds the eight services plus the `outbox-relay` and `payment-events`
workloads that carry the payment event path.
