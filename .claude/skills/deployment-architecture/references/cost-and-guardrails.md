# Cost model & guardrails

Cost is a **first-class design constraint** here, not an afterthought. The hard ceiling is **$20/mo of real spend**; the $200 promotional credits are treated as *runway, not a burn budget*. At ~$17–19/mo projected, a 3–4 month project costs **$50–75 total**, fully covered — but only because the guardrails below hold.

---

## The budget module runs before any compute

`deploy/terraform/modules/budget/` is applied **first**, before the VPC, before the instance. That ordering is the point: the alarm must exist before there is anything that can spend.

- **One monthly COST budget**, limit = the **panic ceiling** ($40, `var.monthly_budget_usd`). One budget keeps it in the AWS Budgets free tier (first two are free).
- Three notifications to `var.alert_email`: **50% actual** ($20 — the real target), **100% actual** ($40), and **100% forecast** (we're *going* to blow it).
- **Cost Anomaly Detection**: a DIMENSIONAL monitor on SERVICE, daily subscription, alerting on anomalies ≥ $5.

**Set the budget on actual/unblended cost, not the credit-inclusive view.** This is the subtle one: with credits applied, the "your bill" number can read ~$0 while resources quietly accrue real charges against the credit balance. A budget watching the credit-inclusive figure would stay silent until the day the credits run dry — and then the bill arrives all at once. Watching unblended spend means the alarm fires on the day a resource is forgotten, not months later.

**Record the credit expiry date** (Billing → Credits) in the runbook. That date is when "effectively free" ends and the toggle-off discipline goes from good habit to real money.

---

## Where the money actually goes (Rung 2, us-east-1)

| Line item | Est. monthly | Notes |
|---|---|---|
| EC2 `t3.large` on-demand, toggled (~short weekday sessions) | ~$2.75–12.50 | **scales with hours you leave it on** |
| EBS gp3 50GB | ~$4 | **billed even while the instance is stopped** — the dominant persistent cost |
| Public IPv4 (ephemeral, while running) | ~$0.75 | see the trap below |
| DynamoDB on-demand | ~$0.50 | ~free at learning scale |
| ECR storage (~3GB) | ~$0.30 | lifecycle policy keeps it lean |
| Data transfer out | ~$0 | first 100GB/mo free |
| **Rung 2 total** | **~$8** | |
| EKS weekends (control plane + spot nodes + ALB, destroy-discipline) | ~$9–11 | only in Phase B+ |
| CI (GitHub Actions free tier) | $0 | |

**Always-on `t3.large` on-demand ≈ $66/mo.** The toggle-off switch is *the* thing that makes this affordable — it is not an optimisation, it's load-bearing.

**Swing factors:** EBS size and EKS session count. Everything else is noise.

---

## The four traps

**1 — Elastic IPs bill while stopped.** Since Feb 2024 AWS charges ~$0.005/hr (~$3.60/mo) per public IPv4 — including an EIP **that is attached to a stopped instance**. A parked EIP would silently negate the entire saving from stopping the box. **Design rule: use the ephemeral auto-assigned public IP** (bills only while running, ~$0.75/mo). The cost is that the IP changes on every start — accepted deliberately, and why `make start-ec2-k3s` re-prints the SSH command and the host key isn't pinned.

**2 — NAT gateways.** ~$32/mo for a resource that does nothing but let private subnets reach the internet. The VPC has **none**: the box sits in a public subnet behind a security group that only admits port 22 from one IP. If anyone proposes private subnets, they're proposing a NAT gateway — make that cost explicit first.

**3 — Orphaned load balancers (Rung 3).** An ALB created by the Load Balancer Controller in response to a k8s `Ingress` is *not* in Terraform state and *survives* `terraform destroy`. Delete the k8s objects first. This is half of Gate 3.

**4 — EBS volumes from PVCs.** Dynamically provisioned volumes with a `Retain` policy outlive the cluster. `helm uninstall` before destroying, then check for unattached volumes in the console.

---

## The shutdown ritual — every session, no exceptions

1. `make -C deploy stop-ec2-k3s` (or `terraform destroy` for the EKS env).
2. Confirm in the console that nothing billable is left: no running instance, no orphan LB / EIP / NAT / unattached volume.
3. **Never** leave either environment up overnight. **Never** run k3s-EC2 and EKS at the same time.
4. Weekly: glance at AWS Budgets (unblended) + Cost Explorer; keep the credit-expiry date in view.

---

## Deliberate cost decisions worth knowing

These come up whenever someone asks "why not just…":

| Decision | Saves | Costs |
|---|---|---|
| No NAT gateway; public subnet + tight SG | ~$32/mo | egress goes out the instance's public IP; SG is the only barrier |
| Ephemeral IP over Elastic IP | ~$2.85/mo | IP changes every start — fresh tunnel each session |
| Redpanda instead of Kafka + Zookeeper | ~2GB RAM (→ smaller instance) | one less "real Kafka" line on the résumé; wire-compatible so clients are unchanged |
| Temporal with SQL visibility, no Elasticsearch | ~1–2GB RAM | no advanced workflow search |
| 1 Postgres with 4 databases, 1 Redis | 4 containers | a single failure domain for all four services' data |
| `us-east-1` despite the author being in Vietnam | ~15–20% vs `ap-southeast-1` | latency, irrelevant for kubectl/SSH-driven learning |
| Real DynamoDB instead of LocalStack | — | ~$0.50/mo, and it *adds* genuine IAM learning — this is why Rung 1.5 was retired |
| `t3.large` on-demand as default | predictability | 8GB is tight; the documented fallback is **`t3.xlarge` on spot — more RAM for less money** (~$11.5/mo) |

**Why spot is acceptable on a single stateful box here** (it normally isn't): all persistent data is already on EBS (survives interruption) or in DynamoDB (off-box), and the box is already treated as ephemeral-when-off. A spot interruption is functionally a `make stop` you didn't trigger. The only real downside is an occasional "capacity unavailable" on start — fine for a learning environment.

**Graviton (`t4g`) is a deferred ~20% lever, not a gate.** It requires arm64 rebuilds of every app *and* infra image; the Rung 1.5 work already showed architecture friction is non-trivial. Documented as a future optimisation.
