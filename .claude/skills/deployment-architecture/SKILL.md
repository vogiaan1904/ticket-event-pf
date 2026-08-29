---
name: deployment-architecture
description: Use when working on, explaining, or extending how TicketBottle is deployed to AWS — the local kind → k3s-on-EC2 → EKS ladder, the Terraform under deploy/terraform, the Helm values overlays, the GitHub-OIDC→ECR pipeline, instance-profile/IRSA credentials, or the cost guardrails. Also use when asked "how is this deployed", "what runs where", "what changes for EKS", or when drawing/updating an infrastructure diagram.
---

# How TicketBottle is deployed

One Helm chart, three targets. The **application topology never changes** — the deploy target is chosen by a values overlay plus a Terraform delta. That portability is the whole point: manifests authored for free on the laptop are the same ones that run on real AWS.

```
                    deploy/helm/ticketbottle/   (one chart, one app topology)
                                  |
      +---------------------------+---------------------------+
      |                           |                           |
 values-local.yaml          values-k3s.yaml            values-eks.yaml
 Rung 1 · kind              Rung 2 · k3s on EC2        Rung 3 · EKS
 $0, offline fallback       ~$8/mo, THE workbench      ~$10/weekend, $0 destroyed
 local images               ECR images                 ECR images
 dynamodb-local pod         real DynamoDB              real DynamoDB
 NodePort 30000             NodePort + SSH tunnel      ALB ingress
 static dummy creds         EC2 instance profile       IRSA
 BUILT ✅  (Gate 1)         BUILT ✅  (Gate 2)          BUILT ✅  (Gate 3)
```

**Current state (2026-08-24). All three rungs are built and all three gates are green.** Rung 2 remains the everyday environment: the full purchase flow runs on the k3s box and a stop/start cycle preserves data. **Rung 3 has completed a full create → deploy → prove → destroy cycle** — Gate 3a passed on 2026-08-24 (order `TB-GATE1-20260824-JCGVD4YX` is still in the real DynamoDB table, `COMPLETED`), and Gate 3b passed with all ten leak checks `OK`. The teardown tooling exists: `eks-teardown.sh`, `eks-leak-check.sh`, `eks-sweep-orphans.sh` and a full Makefile Rung 3 section. Rung 1.5 (LocalStack) was built, then **retired** in favour of real DynamoDB — don't resurrect it; the dormant `values-localstack.yaml` and `templates/infra/localstack-bridge.yaml` render nothing on any live target.

> **Before assuming EKS is off, check.** The cluster has no stop switch and bills $0.10/hr from `apply` to `destroy`:
> `aws eks list-clusters --region us-east-1` — an empty list is the only proof it's off. The off switch is `make -C deploy eks-down` (ordered teardown, then the leak check).

> **Two scaling limits are known from the code and neither is fixed by adding replicas** — a 100-connection Postgres ceiling that 4 `inventory-service` replicas exhaust on their own, and a `SELECT … FOR UPDATE` row lock that caps throughput on one hot ticket class. **Do not autoscale `inventory-service`.** Details in the Rung 3 reference § scaling limits.

## Pick your reference

| You are… | Read |
|---|---|
| Explaining / changing what runs today, debugging the box, touching `deploy/terraform` or `values-k3s.yaml` | [references/rung2-ec2-k3s.md](references/rung2-ec2-k3s.md) |
| Running or tearing down the EKS rung, touching `deploy/terraform/envs/eks` or `values-eks.yaml`, or asked "what changes on real managed Kubernetes?" | [references/rung3-eks.md](references/rung3-eks.md) |
| Touching anything that costs money, or asked "why is this so cheap / what could blow the budget?" | [references/cost-and-guardrails.md](references/cost-and-guardrails.md) |

## File map

| Piece | Where |
|---|---|
| Why the ladder is shaped this way (rungs, trade-offs) | [references/rung2-ec2-k3s.md](references/rung2-ec2-k3s.md), [references/rung3-eks.md](references/rung3-eks.md) |
| Account-wide infra (budget, VPC, ECR, DynamoDB, CI IAM) | `deploy/terraform/envs/foundation/` |
| The k3s box itself | `deploy/terraform/envs/k3s/` + `deploy/terraform/modules/ec2-k3s/` |
| The EKS cluster | `deploy/terraform/envs/eks/` + `deploy/terraform/modules/{eks,irsa-role}/` |
| Helm chart + per-target overlays | `deploy/helm/ticketbottle/` |
| EKS cluster-side bootstrap + deploy | `deploy/scripts/eks-bootstrap.sh`, `deploy/scripts/eks-deploy.sh`, `deploy/k8s/eks/storageclass-gp3.yaml` |
| EKS teardown + proof it's gone | `deploy/scripts/eks-teardown.sh`, `eks-leak-check.sh`, `eks-sweep-orphans.sh` (recovery) |
| Image build → ECR | `.github/workflows/build-push-ecr.yml` |
| Day-to-day operations (stop/start/kubeconfig/gate/teardown) | `deploy/Makefile` — Rung 2 and Rung 3 sections |
| Architecture diagram | `assets/architecture-aws.png` (drawio source is local-only, gitignored) |

## The three invariants

Break these and the ladder stops being a ladder.

**1 — The chart is authored once; targets are overlays.** A new deploy target must be a `values-*.yaml` + Terraform delta, never a forked manifest. If you find yourself editing `templates/` to make one target work, you're about to introduce drift — parameterise instead (see how `tb.image` made the registry values-driven for Rung 2).

**2 — No long-lived AWS keys anywhere.** CI authenticates by **GitHub OIDC** (`AssumeRoleWithWebIdentity`); the box authenticates by **EC2 instance profile** via IMDSv2; EKS authenticates by **IRSA** (per-ServiceAccount, via the cluster's OIDC provider — built, see `modules/irsa-role`). Each is a different rung of the same lesson — a workload proving its identity instead of holding a secret. The trap that makes this concrete is in the Rung 2 reference (§ credential chain).

**3 — Everything is toggle-off-able.** Rung 2 is one `stop-instances` away from ~$0 compute; Rung 3 is one `terraform destroy` away from $0. Any component that can't be switched off, or that bills while stopped (Elastic IPs, NAT gateways, idle load balancers), needs an explicit justification — see the cost reference.

## Where the app architecture lives (not here)

This skill covers *deployment*. For the system it deploys — the seven services, the saga, the queue, the outbox — see root `CLAUDE.md`, `docs/ARCHITECTURE.md`, and the `trace-purchase-flow` / `outbox-relay` skills.
