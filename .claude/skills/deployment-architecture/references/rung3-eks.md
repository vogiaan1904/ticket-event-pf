# Rung 3 — ephemeral EKS (DESIGNED, NOT BUILT)

**Status:** no `deploy/terraform/modules/eks/`, no `envs/eks/`, no `values-eks.yaml` exists yet. This is Phase B of the roadmap (weeks 4–6), gated on Gate 3. Everything below is the *design* plus the parts of Rung 2 that were already built to accommodate it — do not describe it as running.

**The model is a weekend sprint, not an environment.** `terraform apply` Friday, learn, `terraform destroy` Sunday. It is *never* run simultaneously with the k3s box. EKS was explicitly **rejected as the everyday environment** — its ~$73/mo control-plane floor would consume the entire budget for learning that a couple of weekend sessions already deliver.

---

## What actually changes from Rung 2

The chart is the same. The app topology is the same. Four things change, and each one is a distinct AWS lesson:

| Concern | Rung 2 (k3s) | Rung 3 (EKS) | The lesson |
|---|---|---|---|
| **Control plane** | k3s process on the instance you own | AWS-managed, multi-AZ, you never see the masters | managed vs self-hosted trade-off |
| **Nodes** | the same one instance | managed node group on **spot**, across the 2 existing public subnets | node lifecycle, spot interruption, drain |
| **Ingress** | NodePort 30000 + SSH tunnel | **ALB** via the AWS Load Balancer Controller, real public hostname | `Ingress` → cloud LB, target groups, health checks |
| **AWS credentials** | EC2 **instance profile** (node-wide) | **IRSA** — per-ServiceAccount IAM role via OIDC | per-workload least privilege |

The last row is the real prize. On Rung 2, *every pod on the box* inherits the node's DynamoDB permissions because credentials come from IMDS. IRSA binds a role to a **ServiceAccount**, so only `order-service` can touch the orders table — the same OIDC federation trick the CI pipeline already uses, applied inside the cluster.

---

## What Rung 2 already pre-wired for this

Two decisions in the built infrastructure exist *only* to make Rung 3 a delta rather than a rewrite:

1. **Two public subnets across two AZs** (`10.0.1.0/24` in AZ-a, `10.0.2.0/24` in AZ-b), even though the k3s box only occupies the first. EKS requires subnets in ≥2 AZs; an ALB needs ≥2 to place its ENIs.
2. **`kubernetes.io/role/elb = "1"` on both subnets** — the tag the AWS Load Balancer Controller uses to *discover* where it may place public load balancers. Without it the controller can't create an ALB and fails with a confusing "no subnets found" error.

Also already reusable as-is: ECR (both rungs pull the same images), the DynamoDB table, the budget module, and the entire Helm chart.

---

## The build order when Phase B starts

```
modules/eks/  (cluster + spot managed node group + OIDC provider for IRSA)
  → envs/eks/  (root module, reads foundation remote state like envs/k3s does)
  → AWS Load Balancer Controller (Helm, needs its own IRSA role)
  → IRSA role for order-service (DynamoDB, scoped to the table)
  → values-eks.yaml  (ALB ingress annotations, serviceAccount names, spot nodeSelector/tolerations)
  → deploy the chart → purchase flow green
  → terraform destroy → verify NO leaked billables → Gate 3
```

**`values-eks.yaml` is expected to be small** — the same overlay shape as `values-k3s.yaml`:
- `image.registry` still injected via `--set` (same ECR).
- `dynamodb.enabled: false`, `order.dynamodbEndpoint: ""` — **identical to k3s**; the difference is *where the credentials come from*, not any app config. Empty env creds fall through the chain to step 3 (`AWS_WEB_IDENTITY_TOKEN_FILE`, injected by the IRSA webhook) instead of step 4 (IMDS). The same "omit the env vars entirely" rule from Rung 2 is what makes this work.
- The gateway's `Service` needs to stop being a NodePort and gain an `Ingress` — this is the one place `_appservice.tpl` will likely need parameterising. Do it as a values-driven switch, not a forked template.

---

## Gate 3 — and why the destroy half is the hard half

1. Full purchase flow green on EKS.
2. `terraform destroy` leaves **no leaked billable resources**.

Half of Gate 3 is a *cleanup* test, deliberately. The classic EKS bill-after-destroy comes from resources Terraform never created and therefore never deletes:

- **ALBs and their security groups** created by the Load Balancer Controller in response to an `Ingress` object. Delete the k8s `Ingress`/`Service type=LoadBalancer` **before** `terraform destroy`, or the ALB is orphaned and bills forever.
- **EBS volumes** from dynamically provisioned PVCs (the infra tier's StatefulSets) — `Retain` reclaim policies survive cluster deletion.
- **NAT gateways / EIPs** if a private-subnet layout is introduced. The current VPC has none; keep it that way unless there's a reason.
- CloudWatch log groups.

**Checklist before every `destroy`:** `kubectl delete ingress,svc --all -n ticketbottle` → wait for the ALB to disappear from the console → `helm uninstall` (drops PVCs) → `terraform destroy` → confirm in the console: no instances, no LBs, no unattached volumes, no EIPs.

---

## Beyond Gate 3 (Phases C–D, sketched only)

The point of having EKS at all is the depth it unlocks:

- **HPA + load test** the virtual queue and inventory under real concurrency.
- **Spot-interruption recovery** — kill a node and watch the Temporal saga and outbox redelivery actually earn their keep. This is the best possible demonstration that the architecture's complexity was buying something.
- **Observability** — Container Insights or Prometheus/Grafana tracing one purchase across all seven services.
- **IRSA least-privilege per service**, not one shared role.
- Optional: Karpenter, ACM + Route 53 TLS on the ALB, External Secrets Operator + Secrets Manager, PITR/backups.

---

## If asked to compare k3s vs EKS

Be honest about the trade, don't sell EKS:

| | k3s on one EC2 | EKS |
|---|---|---|
| Cost floor | ~$8/mo, ~$0 when stopped | ~$73/mo control plane alone, $0 only when destroyed |
| Off switch | `stop-instances`, 30s, data survives | `terraform destroy`, minutes, must rebuild state |
| HA | none — one node | multi-AZ control plane, replaceable nodes |
| Ingress | NodePort + SSH tunnel | real ALB with a public hostname |
| Credentials | node-wide instance profile | per-ServiceAccount IRSA |
| K8s API | identical (k3s is CNCF-conformant) | identical |
| Best for | daily development on a budget | proving managed-Kubernetes and cloud-native ops |

The API surface being *identical* is exactly why the ladder works: everything learned about Deployments, StatefulSets, PVCs, Services and Helm on the $8/mo box transfers unchanged.
