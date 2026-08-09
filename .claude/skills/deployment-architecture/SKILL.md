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
 BUILT ✅  (Gate 1)         BUILT ✅  (Gate 2)          NOT BUILT ⛔ (Gate 3)
```

**Current state (2026-07-27): Rung 2 is the live everyday environment.** Gate 2 is green — the full purchase flow runs on the k3s box, and a stop/start cycle preserves data. Rung 3 (EKS) is designed and partly pre-wired but not built. Rung 1.5 (LocalStack) was built, then **retired** in favour of real DynamoDB — don't resurrect it.

## Pick your reference

| You are… | Read |
|---|---|
| Explaining / changing what runs today, debugging the box, touching `deploy/terraform` or `values-k3s.yaml` | [references/rung2-ec2-k3s.md](references/rung2-ec2-k3s.md) |
| Planning the EKS rung, or asked "what would change on real managed Kubernetes?" | [references/rung3-eks.md](references/rung3-eks.md) |
| Touching anything that costs money, or asked "why is this so cheap / what could blow the budget?" | [references/cost-and-guardrails.md](references/cost-and-guardrails.md) |

## File map

| Piece | Where |
|---|---|
| Plan of record (design + Appendices A/B) | `docs/superpowers/specs/2026-07-09-aws-affordable-deployment-ladder-design.md` |
| Phase A implementation plan (what built Rung 2) | `docs/superpowers/plans/2026-07-20-aws-deploy-phaseA-k3s-ec2-gate2.md` |
| Hands-on labs for Rung 2 (7 labs, concept-first) | `docs/labs/aws-rung2/` |
| Account-wide infra (budget, VPC, ECR, DynamoDB, CI IAM) | `deploy/terraform/envs/foundation/` |
| The k3s box itself | `deploy/terraform/envs/k3s/` + `deploy/terraform/modules/ec2-k3s/` |
| Helm chart + per-target overlays | `deploy/helm/ticketbottle/` |
| Image build → ECR | `.github/workflows/build-push-ecr.yml` |
| Day-to-day operations (stop/start/kubeconfig/gate) | `deploy/Makefile` (Rung 2 section) |
| Architecture diagram | `docs/diagrams/` |

## The three invariants

Break these and the ladder stops being a ladder.

**1 — The chart is authored once; targets are overlays.** A new deploy target must be a `values-*.yaml` + Terraform delta, never a forked manifest. If you find yourself editing `templates/` to make one target work, you're about to introduce drift — parameterise instead (see how `tb.image` made the registry values-driven for Rung 2).

**2 — No long-lived AWS keys anywhere.** CI authenticates by **GitHub OIDC** (`AssumeRoleWithWebIdentity`); the box authenticates by **EC2 instance profile** via IMDSv2; EKS would use **IRSA**. Each is a different rung of the same lesson — a workload proving its identity instead of holding a secret. The trap that makes this concrete is in the Rung 2 reference (§ credential chain).

**3 — Everything is toggle-off-able.** Rung 2 is one `stop-instances` away from ~$0 compute; Rung 3 is one `terraform destroy` away from $0. Any component that can't be switched off, or that bills while stopped (Elastic IPs, NAT gateways, idle load balancers), needs an explicit justification — see the cost reference.

## Where the app architecture lives (not here)

This skill covers *deployment*. For the system it deploys — the seven services, the saga, the queue, the outbox — see root `CLAUDE.md`, `docs/ARCHITECTURE.md`, and the `trace-purchase-flow` / `outbox-relay` skills.
