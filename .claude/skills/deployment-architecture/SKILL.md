---
name: deployment-architecture
description: Use when working on, explaining, or extending how TicketBottle is deployed to AWS — the local kind / k3s-on-EC2 / EKS targets, the Terraform under deploy/terraform, the Helm values overlays, the GitHub-OIDC→ECR pipeline, instance-profile/IRSA credentials, or the cost guardrails. Also use when asked "how is this deployed", "what runs where", "what changes for EKS", or when drawing/updating an infrastructure diagram.
---

# How TicketBottle is deployed

One Helm chart, three targets. The **application topology never changes** — the deploy target is chosen by a values overlay plus a Terraform delta. That portability is the point: the manifests authored for free on a laptop are the ones that run on real AWS.

```
                    deploy/helm/ticketbottle/   (one chart, one app topology)
                                  |
      +---------------------------+---------------------------+
      |                           |                           |
 values-local.yaml          values-k3s.yaml            values-eks.yaml
 kind                       k3s on one EC2             Amazon EKS
 $0, offline                ~$8/mo, stoppable          hourly, ephemeral
 local images               ECR images                 ECR images
 dynamodb pod               real DynamoDB              real DynamoDB
 NodePort 30000             NodePort + SSH tunnel      ALB ingress
 static dummy creds         EC2 instance profile       IRSA
```

**k3s on EC2 is the everyday environment**: it runs the full purchase flow, and a stop/start cycle preserves data on EBS. **EKS is ephemeral** — created for a session and destroyed after, never left standing. The teardown path is real tooling, not a manual checklist: `eks-teardown.sh`, `eks-leak-check.sh`, `eks-sweep-orphans.sh`, and the EKS section of `deploy/Makefile`.

A LocalStack target was built and then **retired** in favour of real DynamoDB — don't resurrect it. `values-localstack.yaml` and `templates/infra/localstack-bridge.yaml` are dormant and render nothing on any live target.

> **Never assume EKS is off — check.** The cluster has no stop switch and bills $0.10/hr from `apply` to `destroy`:
> `aws eks list-clusters --region us-east-1` — an empty list is the only proof. The off switch is `make -C deploy eks-down` (ordered teardown, then the leak check).

> **The k3s box and EKS must never run at the same time.** `make eks-up` calls `eks-guard`, which refuses unless the box is stopped.

> **Two scaling limits follow from the code and neither is fixed by adding replicas** — a 100-connection Postgres ceiling that four `inventory-service` replicas exhaust on their own, and a `SELECT … FOR UPDATE` row lock that caps throughput on a single hot ticket class. **Do not autoscale `inventory-service`.** Details in the EKS reference § scaling limits.

## Pick your reference

| You are… | Read |
|---|---|
| Explaining or changing what runs day to day, debugging the box, touching `deploy/terraform` or `values-k3s.yaml` | [references/k3s-ec2.md](references/k3s-ec2.md) |
| Running or tearing down EKS, touching `deploy/terraform/envs/eks` or `values-eks.yaml`, or asked "what changes on managed Kubernetes?" | [references/eks.md](references/eks.md) |
| Touching anything that costs money, or asked "why is this cheap / what could blow the budget?" | [references/cost-and-guardrails.md](references/cost-and-guardrails.md) |

## File map

| Piece | Where |
|---|---|
| Why each target is shaped this way, and the trade-offs | [references/k3s-ec2.md](references/k3s-ec2.md), [references/eks.md](references/eks.md) |
| Account-wide infra (budget, VPC, ECR, DynamoDB, CI IAM) | `deploy/terraform/envs/foundation/` |
| The k3s box itself | `deploy/terraform/envs/k3s/` + `deploy/terraform/modules/ec2-k3s/` |
| The EKS cluster | `deploy/terraform/envs/eks/` + `deploy/terraform/modules/{eks,irsa-role}/` |
| Helm chart + per-target overlays | `deploy/helm/ticketbottle/` |
| EKS cluster-side bootstrap + deploy | `deploy/scripts/eks-bootstrap.sh`, `deploy/scripts/eks-deploy.sh`, `deploy/k8s/eks/storageclass-gp3.yaml` |
| EKS teardown + proof it's gone | `deploy/scripts/eks-teardown.sh`, `eks-leak-check.sh`, `eks-sweep-orphans.sh` (recovery) |
| Image build → ECR | `.github/workflows/build-push-ecr.yml` |
| Day-to-day operations (stop/start/kubeconfig/gate/teardown) | `deploy/Makefile` — the k3s and EKS sections |
| IP allowlists after a network change | `make -C deploy my-ip`, `update-my-ip`, `k3s-allow-ip`, `eks-allow-ip` |
| Architecture diagram | `assets/architecture-aws.png` (drawio source is local-only, gitignored) |

## The three invariants

Break these and the targets stop being interchangeable.

**1 — The chart is authored once; targets are overlays.** A new deploy target must be a `values-*.yaml` plus a Terraform delta, never a forked manifest. Editing `templates/` to make one target work introduces drift — parameterise instead (see how `tb.image` made the registry values-driven).

**2 — No long-lived AWS keys anywhere.** CI authenticates by **GitHub OIDC** (`AssumeRoleWithWebIdentity`); the box authenticates by **EC2 instance profile** via IMDSv2; EKS authenticates by **IRSA**, per-ServiceAccount via the cluster's OIDC provider (`modules/irsa-role`). All three are the same idea — a workload proving its identity rather than holding a secret. The trap that makes this concrete is in the k3s reference (§ credential chain).

**3 — Everything is toggle-off-able.** The k3s box is one `stop-instances` away from ~$0 compute; EKS is one `terraform destroy` away from $0. Any component that cannot be switched off, or that bills while stopped (Elastic IPs, NAT gateways, idle load balancers), needs an explicit justification — see the cost reference.

## Where the app architecture lives (not here)

This skill covers *deployment*. For the system it deploys — the seven services, the saga, the queue, the outbox — see root `CLAUDE.md`, `docs/ARCHITECTURE.md`, and the `trace-purchase-flow` / `outbox-relay` skills.
