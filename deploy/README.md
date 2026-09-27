# TicketBottle Deployment

Portable Helm chart for the TicketBottle stack. One chart deploys to every target;
the target is selected by a `values-*.yaml` overlay, never by forking a template.

| Target | Overlay | Images | Orders store | Ingress |
|--------|---------|--------|--------------|---------|
| k3s on EC2 | `values-k3s.yaml` | ECR | DynamoDB | NodePort 30000, over an SSH tunnel |
| Amazon EKS | `values-eks.yaml` | ECR | DynamoDB | ALB |

Infrastructure for both lives in `deploy/terraform/`. Images are built only by CI
(`.github/workflows/build-push-ecr.yml`, on push to `main` or `dev`); nothing builds
on a workstation. For single-service work, run that service natively against its
own `docker-compose.dev.yml`.

## Prerequisites
kubectl, helm, the AWS CLI (logged in), and `deploy/secrets.values.yaml`
(`make -C deploy secrets-init`).

## Run the stack on k3s
```bash
make -C deploy start-ec2-k3s    # prints the SSH tunnel command -- run it in its own terminal
make -C deploy k3s-kubeconfig
make -C deploy k3s-deploy       # helm upgrade from the :dev images in ECR
make -C deploy k3s-gate2        # end-to-end purchase-flow acceptance test
make -C deploy stop-ec2-k3s     # the cost switch: compute stops, EBS and data survive
```

## What runs (infra tier)
| Component | Service:port | Notes |
|-----------|--------------|-------|
| Postgres | postgres:5432 | one instance, 4 app DBs + Temporal's 2 |
| Redis | redis:6379 | waitroom (DB0) + gateway auth (DB1) |
| Redpanda | redpanda:9093 | Kafka-API broker (replaces Kafka+ZK) |
| Temporal | temporal:7233 | Postgres visibility, no Elasticsearch |

The app tier adds the eight services plus the `outbox-relay` and `payment-webhook`
workloads that carry the payment event path.
