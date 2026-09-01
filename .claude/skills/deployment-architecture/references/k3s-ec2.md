# k3s on a single stoppable EC2

The everyday environment. One `t3.large` in a public subnet runs *the entire stack* — all 10 app workloads plus Postgres, Redis, Redpanda and Temporal as Kubernetes workloads. The only things outside the box are the things AWS runs better and cheaper than a pod: **DynamoDB**, **ECR**, and **S3** (Terraform state).

**Why one box and not ECS/EKS/Lightsail:** three constraints have to hold at once — a real Kubernetes API, ≤$20/mo, and a single switch that stops all of it. Only k3s-on-one-EC2 satisfies all three: one `stop-instances` halts every datastore and every service together, because they share a machine, and the data survives on EBS. EKS is rejected as the *daily* environment on its ~$73/mo control-plane floor alone, not on merit.

---

## The two Terraform root modules

State lives in an S3 backend (`backend "s3" {}`, config passed at `init`). `envs/k3s` reads `envs/foundation` through `terraform_remote_state` — so the box can be destroyed and rebuilt without touching the account-wide resources.

### `envs/foundation` — account-wide, long-lived

| Module | What it creates | Notes |
|---|---|---|
| `budget` | One monthly COST budget + Cost Anomaly Detection | **Applied before any compute.** Limit is the *panic ceiling* ($40); notifications at 50% ($20, the real target), 100% actual, and 100% forecast. One budget keeps it inside the free tier. |
| `vpc` | `10.0.0.0/16`, IGW, **2 public subnets** (`10.0.1.0/24`, `10.0.2.0/24`) in the first two AZs, one public route table | **No NAT gateway** — saves ~$32/mo. Subnets are tagged `kubernetes.io/role/elb=1` so a future EKS ALB controller can discover them. |
| `ecr` | 13 repositories under `ticketbottle/*` | `scan_on_push`, MUTABLE tags (we push `:latest` *and* `:sha-…`), lifecycle: expire untagged after 3 days, keep last 10 tagged. |
| `dynamodb` | Table `ticketbottle-orders` | `PAY_PER_REQUEST`, `PK`/`SK`, plus `GSI1` and `GSI2` (both projection `ALL`). Single-table design for `order-svc`. |
| `iam_ci` | GitHub OIDC provider + role `ticketbottle-github-actions-ecr` | Thumbprint fetched dynamically via the `tls` provider so it survives GitHub cert rotations. Trust policy: `repo:<owner>/<repo>:*` (any branch), `aud=sts.amazonaws.com`. |

### `envs/k3s` — the disposable box

Just `module.ec2_k3s`, fed the VPC id, the **first** public subnet, and the DynamoDB table ARN from foundation's outputs.

---

## The `ec2-k3s` module, piece by piece

| Resource | Detail | Why it's like that |
|---|---|---|
| AMI | Amazon Linux 2023, x86_64, resolved from the **SSM public parameter** | Never a hard-coded AMI id — those are region-specific and go stale. |
| `aws_instance` | `t3.large`, **50GB gp3** root volume (`root_volume_gb`), `associate_public_ip_address = true` | **Ephemeral** auto-assigned IP, deliberately *not* an Elastic IP — an EIP bills 24/7 even while the instance is stopped (see cost reference). The IP therefore **changes on every start**. |
| `metadata_options` | `http_tokens = "required"` (IMDSv2), `hop_limit = 2` | IMDSv2 blocks the classic SSRF→credential-theft path. Hop limit 2 lets a *pod* (one extra network hop) still reach IMDS. |
| Security group | Ingress **TCP 22 from `var.my_ip_cidr` only**; all egress | There is **no** public HTTP ingress. Nothing but your own IP can open a socket to this box. |
| IAM role `ticketbottle-k3s-node` | DynamoDB CRUD+Query+Scan scoped to *that table and its indexes*; `ecr:GetAuthorizationToken` on `*` (the API requires it); ECR pull scoped to `repository/ticketbottle/*` | Least privilege with the one unavoidable wildcard called out. |
| Key pair | `ticketbottle-k3s`, from a local public key path | |

### user-data — what happens on first boot

`modules/ec2-k3s/user-data.sh.tftpl`:

1. Installs **k3s** (single node, bundled Traefik) with `--write-kubeconfig-mode 644` so the kubeconfig can be `scp`'d out, waits for the node to be `Ready`, creates namespace `ticketbottle`.
2. Writes `/usr/local/bin/refresh-ecr-secret.sh`, which mints an ECR token from the **instance profile**, writes it as a `docker-registry` secret named `regcred`, and patches the namespace's `default` ServiceAccount with `imagePullSecrets: [regcred]`.
3. Installs a systemd **timer** (`OnBootSec=90`, `OnUnitActiveSec=6h`) to re-run it, then runs it once immediately.

**Why the refresher exists:** ECR authorization tokens expire after 12 hours. A pull secret created once by hand silently rots and every pod goes `ImagePullBackOff` the next day. The timer converts a long-lived-secret problem into a short-lived-token loop backed by the instance's own identity.

---

## The credential chain — the single most important gotcha

The AWS SDK resolves credentials in a **fixed order, first match wins**:

```
1. Env vars  AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY   ← checked FIRST
2. Shared config  ~/.aws/credentials
3. Container / EKS (IRSA)  AWS_WEB_IDENTITY_TOKEN_FILE
4. EC2 instance profile  IMDS 169.254.169.254            ← what the box WANTS
```

On kind, the chart sets `AWS_ACCESS_KEY_ID=local` (fake creds for dynamodb-local). Leave that on the box and the SDK stops at **step 1**, uses the fake key, and DynamoDB auth fails — the instance profile at step 4 never gets a look.

**The fix, and it's subtle:** `values-k3s.yaml` sets the creds to `""`, and `templates/apps/config.yaml` **omits the env vars entirely when empty**. Setting them to an empty string is *not* enough — an empty env var is still *set*, and the chain still stops at step 1. With no env creds and no `~/.aws`, resolution falls through to the instance profile.

`services/order-svc/pkg/dynamodb/client.go` cooperates: it only injects static creds when `DYNAMODB_ENDPOINT != ""`. Empty endpoint → plain `LoadDefaultConfig` → the chain above.

> 90% of "why can't my app reach AWS from EC2?" is a stale env var or `~/.aws` file shadowing the instance profile.

---

## What runs on the box

Namespace `ticketbottle`, from `deploy/helm/ticketbottle`.

**App tier — 10 Deployments** (all from `_appservice.tpl`, one replica each, config from a per-service ConfigMap, TCP readiness probes on the gRPC ports):

| Workload | Port | Service object |
|---|---|---|
| `app-gateway` | 3000 HTTP | **NodePort 30000** — the only exposed one |
| `user-service` | 50052 gRPC | ClusterIP |
| `event-service` | 50053 gRPC | ClusterIP |
| `order-service` | 50054 gRPC | ClusterIP · saga orchestrator |
| `order-consumer` | — | none (Kafka consumer) |
| `payment-service` | 50055 gRPC | ClusterIP |
| `waitroom-service` | 50056 gRPC | ClusterIP |
| `inventory-service` | 50057 gRPC | ClusterIP |
| `outbox-relay` | — | none (LISTEN/NOTIFY relay → Kafka) |
| `payment-webhook` | — | ClusterIP (simulated provider webhook adapter) |

Plus migration **Jobs** (`user-migrate`, `event-migrate`, `payment-migrate`) that run Prisma migrations before the services come up.

**Infra tier — trimmed to fit one box:**

| Component | Kind | Trim vs. the original stack |
|---|---|---|
| PostgreSQL | StatefulSet + PVC (5Gi) | **1 instance, 4 databases** (user/event/payment/inventory) instead of 4 containers |
| Redis | StatefulSet + PVC (1Gi) | 1 instance, logical DBs, instead of 2 |
| Redpanda | StatefulSet + PVC (5Gi) | Kafka-API compatible, **drops Zookeeper** (~1GB vs ~3GB). Sarama/KafkaJS clients unchanged. |
| Temporal | Deployment | **No Elasticsearch** — SQL/Postgres visibility. ES alone wants 1–2GB. |
| DynamoDB | *(absent)* | `dynamodb.enabled: false` — the real AWS table replaces the local pod |

PVCs are backed by k3s's `local-path` provisioner writing to the **gp3 root volume**. That's what makes the stop/start data guarantee work: stopping an EC2 instance preserves its EBS volumes.

---

## `values-k3s.yaml` — the entire delta from local

```yaml
target: k3s
image: { tag: latest, pullPolicy: Always }   # registry injected at deploy via --set
dynamodb: { enabled: false }                 # use the real AWS table
order:
  dynamodbEndpoint: ""                       # empty -> default AWS endpoint
  awsRegion: us-east-1
  awsAccessKeyId: ""                         # empty -> omitted -> instance profile
  awsSecretAccessKey: ""
paymentEvents: { enabled: true }             # in-cluster payment path, same as kind
outboxRelay:   { enabled: true }
postgres: { storage: 5Gi }                   # PVCs sized for the real box
redpanda: { storage: 5Gi }
redis:    { storage: 1Gi }
```

The **registry is deliberately not in git** (it contains the account id) — it's passed at deploy time:

```bash
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle --create-namespace \
  -f deploy/helm/ticketbottle/values-k3s.yaml \
  --set image.registry="${ACCOUNT}.dkr.ecr.us-east-1.amazonaws.com/" --wait --timeout 10m
```

`pullPolicy: Always` matters: tags are mutable (`:latest` moves), so `IfNotPresent` would silently keep a stale image after CI pushes a new one.

---

## Image pipeline — GitHub Actions → ECR

`.github/workflows/build-push-ecr.yml`, on push to `main` touching `services/**` or `deploy/adapters/**` (plus `workflow_dispatch`).

- `permissions: id-token: write` → `configure-aws-credentials@v4` with `role-to-assume: ${{ vars.AWS_CI_ROLE_ARN }}` — **OIDC, no stored keys**.
- A 13-entry matrix: 10 runtime images + 3 Prisma `migrate` images (built from the `builder` **target** of the same Dockerfile).
- Buildx, `cache-from/to: type=gha` scoped per repo, tagged `:latest` **and** `:sha-<commit>`.

Nothing is ever built on a developer machine or on the box. CI is the only builder, which is what keeps both of them thin.

---

## Operating it — the k3s section of `deploy/Makefile`

```bash
make -C deploy start-ec2-k3s     # start + wait + print the fresh ssh command (IP changed!)
make -C deploy k3s-kubeconfig    # scp /etc/rancher/k3s/k3s.yaml -> /tmp/k3s.yaml
# open the tunnel printed above, then:
export KUBECONFIG=/tmp/k3s.yaml
make -C deploy k3s-gate2         # purchase flow against GW=http://localhost:3000/api
make -C deploy stop-ec2-k3s      # THE COST SWITCH — compute billing halts, EBS survives
make -C deploy k3s-ip            # current public IP
```

**Access is SSH-tunnel-only.** `localhost:3000` on the Mac forwards to the box's NodePort 30000 (the gateway). There is no ALB, no public HTTP, no ingress hostname. The `gate1-purchase-flow.sh` script was parameterised with `GW=` precisely so the same acceptance test drives kind and k3s unchanged.

The host key is deliberately **not persisted** (`UserKnownHostsFile=/dev/null` in `k3s-kubeconfig`) — the box's IP is ephemeral and gets recycled by AWS, so pinning it would produce a false MITM warning on every start.

---

## Gate 2 — the acceptance criteria

1. **Gate 2a:** the full purchase flow (Waitroom → Order/Temporal → Inventory → Payment → Kafka → confirm → slot freed) green against the k3s box, pulling images from ECR, writing orders to real DynamoDB.
2. **Gate 2b:** `stop-instances` → `start-instances` → the flow still green **and prior data still present** — proving the PVC-on-EBS story.

## Known rough edges

- **The IP changes on every start.** Every session needs a fresh `terraform refresh` + new tunnel. A Route 53 A-record update hook is the documented nicety, not built.
- **Single node, single replica, no HA.** Intentional here; horizontal scaling and node-failure recovery are properties of the EKS target.
- **Secrets are plain K8s Secrets/ConfigMaps.** External Secrets Operator with AWS Secrets Manager is the documented next step.
- **`payment-webhook` is a simulated provider**, not a real PSP callback. A Lambda + API Gateway payment path is optional future work.
