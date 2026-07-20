# AWS Deploy Phase A — k3s on a stoppable EC2 → Gate 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the full TicketBottle stack on **k3s on a single stoppable EC2**, pulling images from ECR and using **real DynamoDB via the instance profile** (no static keys), and prove **Gate 2**: the full purchase flow is green and a `stop`→`start` cycle preserves data. This makes the EC2 the primary "off-the-Mac" workbench from spec Appendix B.

**Architecture:** Phase 0's foundation (VPC, ECR, DynamoDB, budget, CI) already exists. This phase adds a durable **S3 Terraform backend**, an `ec2-k3s` module (one `t3.large` on-demand instance, 50 GB gp3, ephemeral public IP, an instance profile granting DynamoDB + ECR-pull, k3s installed via user-data, and a host-side ECR-token refresher), and the Helm changes that make image references **values-driven** (point at ECR) and make order-svc use the **default AWS credential chain** (instance profile) for DynamoDB. Access is via a single **SSH tunnel** (`-L 6443` for kubectl, `-L 3000→30000` for the gateway) — so no cluster ports are exposed publicly, the gate script runs unchanged against `localhost:3000`, and an ephemeral public IP that changes on stop/start doesn't break anything.

**Tech Stack:** Terraform ≥ 1.10 (S3 native locking), AWS provider `~> 5.0`, k3s (single node, Traefik default), Helm, AWS (EC2, IAM instance profile, DynamoDB, ECR, S3, SG), `aws`/`kubectl`/`helm` CLIs, SSH.

## Global Constraints

- Inherits **all** Phase 0 Global Constraints (`docs/superpowers/plans/2026-07-20-aws-deploy-phase0-cloud-foundation.md`): region `us-east-1`, `$20/$40` budget, no NAT, `PAY_PER_REQUEST`, `linux/amd64`, tag `{Project=ticketbottle, ManagedBy=terraform, Env=...}`.
- **Phase 0 must be green first** (Gate 0): foundation applied, 13 ECR repos populated by CI with a `latest` tag.
- **State:** foundation + k3s state live in **S3** (`ticketbottle-tfstate-<account>`), introduced in Task 1 **before** the first billable resource. Never commit state.
- **Instance:** `t3.large` **on-demand** default (→ `t3.xlarge` only if Gate 2 shows memory pressure); **50 GB gp3**; **ephemeral public IP** (no Elastic IP — spec A.4); **IMDSv2 required**, `http_put_response_hop_limit = 2`.
- **No cluster ports open to the internet.** SG allows inbound **22 (SSH) from your IP only**. kubectl + gateway reached via SSH local-forwards.
- **DynamoDB auth = instance profile only.** `AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY` env vars are **omitted** on k3s (empty in `values-k3s`), and `DYNAMODB_ENDPOINT` is empty → order-svc's `pkg/dynamodb/client.go` falls through to `LoadDefaultConfig` (IMDS → instance profile).
- **Cost discipline:** `make stop` after every session; never leave the instance running overnight (spec B.6). t3.large idle ≈ $0.083/hr.
- **Payment path:** the in-cluster `payment-webhook` (from the `payment-events` image) + `outbox-relay` both deploy, identical to the kind Gate-1 topology — the webhook is **simulated** by the gate script (as in Gate 1); no public payment callback is in scope.
- **Verification model:** infra + behavioural gate, not unit tests (same as Phase 0).

---

## File Structure

```
deploy/terraform/
  envs/
    foundation/main.tf         # MODIFY: add empty backend "s3" {} + ecr_registry output
    k3s/
      main.tf                  # provider, s3 backend, remote_state(foundation), ec2-k3s module
      variables.tf
      outputs.tf
      terraform.tfvars.example
      # terraform.tfvars       # GITIGNORED (ssh key path, my_ip_cidr, state bucket)
  modules/
    ec2-k3s/
      main.tf                  # SG, IAM role+instance profile, EC2, key pair
      user-data.sh.tftpl       # k3s install + namespace + ECR-refresh timer
      variables.tf
      outputs.tf
deploy/helm/ticketbottle/
  templates/_helpers.tpl       # MODIFY: add tb.image helper
  templates/apps/_appservice.tpl   # MODIFY: image from helper + pullPolicy from values
  templates/apps/{user,event,payment,inventory,waitroom,order,gateway}.yaml  # MODIFY: repo (no :local)
  templates/apps/{payment-events,outbox-relay,migrations}.yaml               # MODIFY: helper image
  templates/apps/config.yaml   # MODIFY: order AWS creds conditional
  values.yaml                  # MODIFY: add image.{registry,tag,pullPolicy}
  values-k3s.yaml              # CREATE: k3s overlay
deploy/
  Makefile                     # MODIFY: k3s-* + stop/start targets
  scripts/gate1-purchase-flow.sh   # MODIFY: parametrize GW (GW=${GW:-...})
```

---

## Task 1: S3 remote backend + migrate foundation state

Durable state before any billable compute — if the Mac dies, `terraform destroy` still works (no orphaned EC2). Uses S3 native locking (no DynamoDB lock table needed).

**Files:**
- Create (AWS): S3 bucket `ticketbottle-tfstate-<account>`
- Modify: `deploy/terraform/envs/foundation/main.tf` (add `backend "s3" {}` + `ecr_registry` output)

**Interfaces:**
- Produces: the state bucket name (used by Task 2's `envs/k3s` backend + remote-state lookup); a new `ecr_registry` foundation output (`<account>.dkr.ecr.us-east-1.amazonaws.com`).

- [ ] **Step 1: Create the state bucket (idempotent CLI)**
```bash
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
BUCKET="ticketbottle-tfstate-${ACCOUNT}"
aws s3api create-bucket --bucket "$BUCKET" --region us-east-1
aws s3api put-bucket-versioning --bucket "$BUCKET" \
  --versioning-configuration Status=Enabled
aws s3api put-public-access-block --bucket "$BUCKET" --public-access-block-configuration \
  BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
aws s3api put-bucket-encryption --bucket "$BUCKET" --server-side-encryption-configuration \
  '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
echo "state bucket: $BUCKET"
```
Expected: bucket created (or already-owned), versioning + encryption + public-access-block applied.

- [ ] **Step 2: Add the S3 backend block to `deploy/terraform/envs/foundation/main.tf`**
Inside the existing `terraform { ... }` block, add:
```hcl
  backend "s3" {}
```
(Empty — values are passed via `-backend-config` so the account id stays out of git.)

- [ ] **Step 3: Add an `ecr_registry` output — append to `deploy/terraform/envs/foundation/outputs.tf`**
```hcl
output "ecr_registry" {
  # host portion shared by every repo, e.g. 123456789012.dkr.ecr.us-east-1.amazonaws.com
  value = split("/", values(module.ecr.repository_urls)[0])[0]
}
```

- [ ] **Step 4: Migrate foundation state to S3**
```bash
cd deploy/terraform/envs/foundation
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
terraform init -migrate-state \
  -backend-config="bucket=ticketbottle-tfstate-${ACCOUNT}" \
  -backend-config="key=foundation/terraform.tfstate" \
  -backend-config="region=us-east-1" \
  -backend-config="encrypt=true" \
  -backend-config="use_lockfile=true"
# answer 'yes' to copy existing state to the new backend
terraform apply   # adds the new ecr_registry output; 0 infra changes
```
Expected: `Successfully configured the backend "s3"`; state copied; apply shows only the new output, `0 to add/change/destroy` of infrastructure.

- [ ] **Step 5: Verify state is remote + capture registry**
```bash
aws s3 ls "s3://ticketbottle-tfstate-${ACCOUNT}/foundation/"   # shows terraform.tfstate
terraform output -raw ecr_registry                             # <account>.dkr.ecr.us-east-1.amazonaws.com
```

- [ ] **Step 6: Commit**
```bash
git add deploy/terraform/envs/foundation/main.tf deploy/terraform/envs/foundation/outputs.tf
git commit -m "feat(aws-phaseA): S3 remote backend + ecr_registry output; migrate foundation state"
```

---

## Task 2: `ec2-k3s` module + `envs/k3s` root → apply

The single stoppable box: SG (SSH-only), an instance profile (DynamoDB + ECR-pull), a `t3.large`/50 GB instance whose user-data installs k3s and a host-side ECR-token refresher.

**Files:**
- Create: `deploy/terraform/modules/ec2-k3s/{main.tf,variables.tf,outputs.tf,user-data.sh.tftpl}`
- Create: `deploy/terraform/envs/k3s/{main.tf,variables.tf,outputs.tf,terraform.tfvars.example}`
- Create (gitignored): `deploy/terraform/envs/k3s/terraform.tfvars`

**Interfaces:**
- Consumes: foundation remote state (`vpc_id`, `public_subnet_ids`, `dynamodb_table_arn`).
- Produces: outputs `public_ip` (string, ephemeral), `instance_id` (string), `ssh_user` (string, `ec2-user`), `ssh_command` (string) — consumed by Task 3 and the Makefile (Task 7).

- [ ] **Step 1: Write `deploy/terraform/modules/ec2-k3s/variables.tf`**
```hcl
variable "vpc_id" { type = string }
variable "subnet_id" { type = string }
variable "dynamodb_table_arn" { type = string }
variable "ssh_public_key_path" {
  type        = string
  description = "Path to your SSH public key, e.g. ~/.ssh/id_ed25519.pub"
}
variable "my_ip_cidr" {
  type        = string
  description = "Your public IP as a /32, allowed for SSH. e.g. 203.0.113.7/32"
}
variable "instance_type" {
  type    = string
  default = "t3.large"
}
variable "root_volume_gb" {
  type    = number
  default = 50
}
variable "tags" {
  type    = map(string)
  default = {}
}
```

- [ ] **Step 2: Write `deploy/terraform/modules/ec2-k3s/main.tf`**
```hcl
data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

resource "aws_key_pair" "this" {
  key_name   = "ticketbottle-k3s"
  public_key = file(var.ssh_public_key_path)
  tags       = var.tags
}

resource "aws_security_group" "this" {
  name        = "ticketbottle-k3s"
  description = "k3s box: SSH from my IP only; all egress"
  vpc_id      = var.vpc_id
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.my_ip_cidr]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = merge(var.tags, { Name = "ticketbottle-k3s" })
}

# Instance profile: DynamoDB on the orders table + its indexes, and ECR pull.
resource "aws_iam_role" "node" {
  name = "ticketbottle-k3s-node"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
  tags = var.tags
}

data "aws_iam_policy_document" "node" {
  statement {
    sid    = "Dynamo"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem",
      "dynamodb:DeleteItem", "dynamodb:Query", "dynamodb:Scan",
      "dynamodb:BatchGetItem", "dynamodb:BatchWriteItem",
      "dynamodb:ConditionCheckItem", "dynamodb:DescribeTable",
    ]
    resources = [var.dynamodb_table_arn, "${var.dynamodb_table_arn}/index/*"]
  }
  statement {
    sid       = "EcrAuth"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }
  statement {
    sid    = "EcrPull"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:GetDownloadUrlForLayer",
      "ecr:BatchGetImage",
    ]
    resources = ["arn:aws:ecr:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:repository/ticketbottle/*"]
  }
}

resource "aws_iam_role_policy" "node" {
  name   = "k3s-node"
  role   = aws_iam_role.node.id
  policy = data.aws_iam_policy_document.node.json
}

resource "aws_iam_instance_profile" "node" {
  name = "ticketbottle-k3s-node"
  role = aws_iam_role.node.name
}

resource "aws_instance" "k3s" {
  ami                         = data.aws_ssm_parameter.al2023.value
  instance_type               = var.instance_type
  subnet_id                   = var.subnet_id
  vpc_security_group_ids      = [aws_security_group.this.id]
  iam_instance_profile        = aws_iam_instance_profile.node.name
  key_name                    = aws_key_pair.this.key_name
  associate_public_ip_address = true
  user_data                   = templatefile("${path.module}/user-data.sh.tftpl", { region = data.aws_region.current.name })

  metadata_options {
    http_tokens                 = "required" # IMDSv2
    http_put_response_hop_limit = 2          # allow pods to reach IMDS if ever needed
  }

  root_block_device {
    volume_type = "gp3"
    volume_size = var.root_volume_gb
    tags        = merge(var.tags, { Name = "ticketbottle-k3s-root" })
  }

  tags = merge(var.tags, { Name = "ticketbottle-k3s" })
}
```

- [ ] **Step 3: Write `deploy/terraform/modules/ec2-k3s/user-data.sh.tftpl`**
```bash
#!/bin/bash
set -euxo pipefail

# --- k3s (single node, default Traefik). Kubeconfig world-readable for SSH fetch. ---
curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="--write-kubeconfig-mode 644" sh -
until /usr/local/bin/kubectl get nodes 2>/dev/null | grep -q ' Ready'; do sleep 5; done
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
/usr/local/bin/kubectl create namespace ticketbottle --dry-run=client -o yaml | /usr/local/bin/kubectl apply -f -

# --- ECR pull-secret refresher (instance profile -> regcred every 6h + on boot) ---
cat >/usr/local/bin/refresh-ecr-secret.sh <<'EOS'
#!/bin/bash
set -euo pipefail
REGION="${region}"
NS=ticketbottle
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
REGISTRY="$${ACCOUNT}.dkr.ecr.$${REGION}.amazonaws.com"
TOKEN=$(aws ecr get-login-password --region "$REGION")
/usr/local/bin/kubectl create namespace "$NS" --dry-run=client -o yaml | /usr/local/bin/kubectl apply -f -
/usr/local/bin/kubectl create secret docker-registry regcred -n "$NS" \
  --docker-server="$REGISTRY" --docker-username=AWS --docker-password="$TOKEN" \
  --dry-run=client -o yaml | /usr/local/bin/kubectl apply -f -
/usr/local/bin/kubectl patch serviceaccount default -n "$NS" \
  -p '{"imagePullSecrets":[{"name":"regcred"}]}'
EOS
chmod +x /usr/local/bin/refresh-ecr-secret.sh

cat >/etc/systemd/system/ecr-refresh.service <<'EOS'
[Unit]
Description=Refresh ECR pull secret
After=k3s.service
[Service]
Type=oneshot
ExecStart=/usr/local/bin/refresh-ecr-secret.sh
EOS

cat >/etc/systemd/system/ecr-refresh.timer <<'EOS'
[Unit]
Description=Refresh ECR pull secret on boot and every 6h
[Timer]
OnBootSec=90
OnUnitActiveSec=6h
[Install]
WantedBy=timers.target
EOS

systemctl daemon-reload
systemctl enable --now ecr-refresh.timer
# run once now so the secret exists before the first helm deploy
/usr/local/bin/refresh-ecr-secret.sh || true
```
Note: `$${...}` escapes are for Terraform's `templatefile` (only `${region}` is interpolated by Terraform; the rest are shell).

- [ ] **Step 4: Write `deploy/terraform/modules/ec2-k3s/outputs.tf`**
```hcl
output "instance_id" { value = aws_instance.k3s.id }
output "public_ip" { value = aws_instance.k3s.public_ip }
output "ssh_user" { value = "ec2-user" }
output "ssh_command" {
  value = "ssh -o StrictHostKeyChecking=accept-new -L 6443:127.0.0.1:6443 -L 3000:127.0.0.1:30000 ec2-user@${aws_instance.k3s.public_ip}"
}
```

- [ ] **Step 5: Write `deploy/terraform/envs/k3s/variables.tf`**
```hcl
variable "state_bucket" { type = string }
variable "ssh_public_key_path" { type = string }
variable "my_ip_cidr" { type = string }
variable "instance_type" {
  type    = string
  default = "t3.large"
}
```

- [ ] **Step 6: Write `deploy/terraform/envs/k3s/main.tf`**
```hcl
terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
  backend "s3" {}
}

provider "aws" {
  region = "us-east-1"
  default_tags {
    tags = { Project = "ticketbottle", ManagedBy = "terraform", Env = "k3s" }
  }
}

data "terraform_remote_state" "foundation" {
  backend = "s3"
  config = {
    bucket = var.state_bucket
    key    = "foundation/terraform.tfstate"
    region = "us-east-1"
  }
}

module "ec2_k3s" {
  source              = "../../modules/ec2-k3s"
  vpc_id              = data.terraform_remote_state.foundation.outputs.vpc_id
  subnet_id           = data.terraform_remote_state.foundation.outputs.public_subnet_ids[0]
  dynamodb_table_arn  = data.terraform_remote_state.foundation.outputs.dynamodb_table_arn
  ssh_public_key_path = var.ssh_public_key_path
  my_ip_cidr          = var.my_ip_cidr
  instance_type       = var.instance_type
  tags                = { Project = "ticketbottle", ManagedBy = "terraform", Env = "k3s" }
}
```

- [ ] **Step 7: Write `deploy/terraform/envs/k3s/outputs.tf`**
```hcl
output "public_ip" { value = module.ec2_k3s.public_ip }
output "instance_id" { value = module.ec2_k3s.instance_id }
output "ssh_command" { value = module.ec2_k3s.ssh_command }
```

- [ ] **Step 8: Write `deploy/terraform/envs/k3s/terraform.tfvars.example`**
```hcl
state_bucket        = "ticketbottle-tfstate-REPLACE_WITH_ACCOUNT_ID"
ssh_public_key_path = "~/.ssh/id_ed25519.pub"
my_ip_cidr          = "REPLACE_WITH_YOUR_IP/32"
instance_type       = "t3.large"
```

- [ ] **Step 9: Fill in the real tfvars**
```bash
cd deploy/terraform/envs/k3s
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
MYIP=$(curl -s https://checkip.amazonaws.com)
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars: state_bucket=ticketbottle-tfstate-$ACCOUNT, my_ip_cidr=$MYIP/32,
# ssh_public_key_path to a real key (create one with: ssh-keygen -t ed25519 if needed)
echo "account=$ACCOUNT  myip=$MYIP"
```

- [ ] **Step 10: Init with the S3 backend**
```bash
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
terraform init \
  -backend-config="bucket=ticketbottle-tfstate-${ACCOUNT}" \
  -backend-config="key=k3s/terraform.tfstate" \
  -backend-config="region=us-east-1" \
  -backend-config="encrypt=true" \
  -backend-config="use_lockfile=true"
```
Expected: backend initialized; `hashicorp/aws` installed.

- [ ] **Step 11: Plan**
```bash
terraform fmt -recursive ../.. && terraform validate && terraform plan
```
Expected: valid; plan shows **~7 to add** (key pair, SG, IAM role, role policy, instance profile, EC2 instance) — confirm the AMI resolves and `my_ip_cidr` is your `/32`.

- [ ] **Step 12: Apply**
```bash
terraform apply   # yes  — THIS STARTS BILLING (~$0.083/hr for t3.large)
terraform output ssh_command
```
Expected: `Apply complete!`; `ssh_command` prints the tunnel command with the instance's public IP.

- [ ] **Step 13: Commit (state is remote; only code)**
```bash
git add deploy/terraform/modules/ec2-k3s deploy/terraform/envs/k3s
git commit -m "feat(aws-phaseA): ec2-k3s module + envs/k3s (SSH-only SG, instance profile, k3s user-data, ECR refresher)"
```

---

## Task 3: Remote kubectl via SSH tunnel; verify k3s

**Files:** none (local kubeconfig + tunnel).

**Interfaces:**
- Consumes: `ssh_command` output; the instance's `/etc/rancher/k3s/k3s.yaml`.
- Produces: a working `kubectl` against the remote k3s through `localhost:6443`.

- [ ] **Step 1: Open the SSH tunnel (leave this terminal running)**
```bash
cd deploy/terraform/envs/k3s
eval "$(terraform output -raw ssh_command)"
# This backgrounds nothing — keep the shell open. (Add -fN to background if preferred.)
```
Expected: an SSH session to the box; local ports 6443 and 3000 now forward to the cluster.

- [ ] **Step 2: Fetch + localize the kubeconfig (in a second terminal)**
```bash
IP=$(cd deploy/terraform/envs/k3s && terraform output -raw public_ip)
scp -o StrictHostKeyChecking=accept-new ec2-user@"$IP":/etc/rancher/k3s/k3s.yaml /tmp/k3s.yaml
# k3s.yaml already points at 127.0.0.1:6443 — perfect for the tunnel. Use it directly:
export KUBECONFIG=/tmp/k3s.yaml
```
Expected: `k3s.yaml` copied; server URL is `https://127.0.0.1:6443`.

- [ ] **Step 3: Verify the cluster + ECR secret**
```bash
kubectl get nodes
kubectl -n ticketbottle get secret regcred
kubectl -n ticketbottle get sa default -o jsonpath='{.imagePullSecrets}'; echo
```
Expected: one node `Ready`; `regcred` exists (type `kubernetes.io/dockerconfigjson`); default SA lists `regcred`. If `regcred` is missing, SSH in and run `sudo /usr/local/bin/refresh-ecr-secret.sh`.

- [ ] **Step 4: No commit** (verification only). Record: cluster reachable via tunnel, pull secret present.

---

## Task 4: Helm changes — values-driven images + instance-profile DynamoDB

Three edits: (4a) image references become `registry + repo + tag` from values; (4b) order-svc AWS creds become conditional (omitted → instance profile); (4c) the `values-k3s.yaml` overlay.

**Files:** `_helpers.tpl`, `_appservice.tpl`, the 8 per-app templates, `payment-events.yaml`, `outbox-relay.yaml`, `migrations.yaml`, `config.yaml`, `values.yaml`, new `values-k3s.yaml`.

**Interfaces:**
- Produces: a chart deployable to either kind (`registry=""`, `tag=local`) or k3s/ECR (`registry=<ecr>/`, `tag=latest`) with no template edits — only values.

### 4a — values-driven image references

- [ ] **Step 1: Add the `tb.image` helper — append to `deploy/helm/ticketbottle/templates/_helpers.tpl`**
```gotemplate
{{- define "tb.image" -}}
{{- $ := .ctx -}}
{{- printf "%s%s:%s" $.Values.image.registry .repo $.Values.image.tag -}}
{{- end -}}
```

- [ ] **Step 2: Add the `image` block — in `deploy/helm/ticketbottle/values.yaml`**, after the `namespace:` line:
```yaml
image:
  registry: ""          # "" for kind; "<account>.dkr.ecr.us-east-1.amazonaws.com/" for k3s (set via --set)
  tag: local            # "local" for kind; "latest"/"sha-..." for k3s
  pullPolicy: IfNotPresent
```

- [ ] **Step 3: Rewrite the image lines in `_appservice.tpl`**
Replace:
```gotemplate
          image: {{ .image }}
          imagePullPolicy: IfNotPresent
```
with:
```gotemplate
          image: {{ include "tb.image" (dict "ctx" $ "repo" .image) }}
          imagePullPolicy: {{ $.Values.image.pullPolicy }}
```

- [ ] **Step 4: Change each per-app include to pass a bare repo (drop `:local`)**
In each file, replace the `"image" "ticketbottle/<x>:local"` fragment with `"image" "ticketbottle/<x>"`:
  - `apps/user.yaml`: `"image" "ticketbottle/user"`
  - `apps/event.yaml`: `"image" "ticketbottle/event"`
  - `apps/payment.yaml`: `"image" "ticketbottle/payment"`
  - `apps/inventory.yaml`: `"image" "ticketbottle/inventory"`
  - `apps/waitroom.yaml`: `"image" "ticketbottle/waitroom"`
  - `apps/gateway.yaml`: `"image" "ticketbottle/gateway"`
  - `apps/order.yaml`: two edits — `"image" "ticketbottle/order-api"` and `"image" "ticketbottle/order-consumer"`

- [ ] **Step 5: Rewrite `apps/payment-events.yaml` image lines**
Replace:
```yaml
          image: ticketbottle/payment-events:local
          imagePullPolicy: IfNotPresent
```
with:
```yaml
          image: {{ include "tb.image" (dict "ctx" . "repo" "ticketbottle/payment-events") }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
```

- [ ] **Step 6: Rewrite `apps/outbox-relay.yaml` image lines**
Replace:
```yaml
          image: ticketbottle/outbox-relay:local
          imagePullPolicy: IfNotPresent
```
with:
```yaml
          image: {{ include "tb.image" (dict "ctx" . "repo" "ticketbottle/outbox-relay") }}
          imagePullPolicy: {{ .Values.image.pullPolicy }}
```

- [ ] **Step 7: Rewrite `apps/migrations.yaml` image lines**
Replace:
```yaml
          image: ticketbottle/{{ $svc }}-migrate:local
          imagePullPolicy: IfNotPresent
```
with:
```yaml
          image: {{ include "tb.image" (dict "ctx" $ "repo" (printf "ticketbottle/%s-migrate" $svc)) }}
          imagePullPolicy: {{ $.Values.image.pullPolicy }}
```

- [ ] **Step 8: Verify local rendering is unchanged (regression guard)**
```bash
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml \
  | grep -E "image: ticketbottle/(user|gateway|payment-events|user-migrate)"
```
Expected: every match still reads `image: ticketbottle/<name>:local` (registry `""` + tag `local` reproduces the old strings exactly).

### 4b — conditional AWS creds for order-svc

- [ ] **Step 9: Make the order AWS creds conditional — in `deploy/helm/ticketbottle/templates/apps/config.yaml`**
In the `order-config` ConfigMap, replace:
```yaml
  AWS_ACCESS_KEY_ID: {{ .Values.order.awsAccessKeyId | quote }}
  AWS_SECRET_ACCESS_KEY: {{ .Values.order.awsSecretAccessKey | quote }}
```
with:
```yaml
  {{- if .Values.order.awsAccessKeyId }}
  AWS_ACCESS_KEY_ID: {{ .Values.order.awsAccessKeyId | quote }}
  AWS_SECRET_ACCESS_KEY: {{ .Values.order.awsSecretAccessKey | quote }}
  {{- end }}
```
Rationale: with empty creds (k3s), the env vars are omitted, so the AWS SDK's env provider yields nothing and the chain falls to the EC2 instance profile (verified in `services/order-svc/pkg/dynamodb/client.go` — static creds are only set when `DYNAMODB_ENDPOINT != ""`).

- [ ] **Step 10: Verify both renderings**
```bash
# local: creds present
helm template tb deploy/helm/ticketbottle -f deploy/helm/ticketbottle/values-local.yaml \
  | grep -A15 "name: order-config" | grep AWS_ACCESS_KEY_ID
# k3s: creds absent (uses the overlay from 4c, created next)
```
Expected: local shows `AWS_ACCESS_KEY_ID: "local"`; after 4c, the k3s render shows **no** `AWS_ACCESS_KEY_ID` line.

### 4c — the k3s overlay

- [ ] **Step 11: Create `deploy/helm/ticketbottle/values-k3s.yaml`**
```yaml
# Rung 2 (k3s on EC2). Layer on base values.yaml:
#   helm upgrade tb deploy/helm/ticketbottle -n ticketbottle \
#     -f deploy/helm/ticketbottle/values-k3s.yaml \
#     --set image.registry=<account>.dkr.ecr.us-east-1.amazonaws.com/
target: k3s

image:
  # registry is injected at deploy time via --set (keeps the account id out of git)
  tag: latest
  pullPolicy: Always

# Use REAL AWS DynamoDB (no in-cluster dynamodb-local pod).
dynamodb:
  enabled: false

order:
  dynamodbEndpoint: ""     # empty -> default AWS endpoint
  awsRegion: us-east-1
  awsAccessKeyId: ""       # empty -> config.yaml omits creds -> instance-profile chain
  awsSecretAccessKey: ""

# In-cluster payment path (simulated webhook + relay), same as Gate 1.
paymentEvents:
  enabled: true
outboxRelay:
  enabled: true

# Modest PVCs sized for the real box.
postgres:
  storage: 5Gi
redpanda:
  storage: 5Gi
redis:
  storage: 1Gi
```

- [ ] **Step 12: Render the full k3s manifest as a dry check**
```bash
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
helm template tb deploy/helm/ticketbottle \
  -f deploy/helm/ticketbottle/values-k3s.yaml \
  --set image.registry="${ACCOUNT}.dkr.ecr.us-east-1.amazonaws.com/" \
  | grep -E "image: .*dkr.ecr|AWS_ACCESS_KEY_ID|dynamodb-local" || true
```
Expected: images show the ECR registry + `:latest`; **no** `AWS_ACCESS_KEY_ID` line; **no** `dynamodb-local` pod.

- [ ] **Step 13: Commit**
```bash
git add deploy/helm/ticketbottle
git commit -m "feat(aws-phaseA): values-driven images + instance-profile DynamoDB + values-k3s overlay"
```

---

## Task 5: Deploy the stack to k3s from ECR

**Files:** none (deploy operation). Uses the tunnel + `KUBECONFIG=/tmp/k3s.yaml` from Task 3.

**Interfaces:**
- Consumes: ECR images (Phase 0 CI), `regcred` (Task 2), the k3s overlay (Task 4).
- Produces: all app pods Ready on k3s, backed by real DynamoDB.

- [ ] **Step 1: Confirm ECR has current images**
```bash
aws ecr describe-images --repository-name ticketbottle/gateway --region us-east-1 \
  --query "imageDetails[?contains(imageTags,'latest')].imagePushedAt" --output text
```
Expected: a recent timestamp. If empty, run the Phase-0 CI workflow first.

- [ ] **Step 2: Deploy (migrations run as post-upgrade hooks)**
```bash
export KUBECONFIG=/tmp/k3s.yaml
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
helm upgrade --install tb deploy/helm/ticketbottle -n ticketbottle --create-namespace \
  -f deploy/helm/ticketbottle/values-k3s.yaml \
  --set image.registry="${ACCOUNT}.dkr.ecr.us-east-1.amazonaws.com/" \
  --wait --timeout 10m
```
Expected: release deployed; `--wait` blocks until Deployments are Ready. First pull is slow (cold ECR pulls onto the box).

- [ ] **Step 3: Verify pods + DynamoDB wiring**
```bash
kubectl -n ticketbottle get pods
kubectl -n ticketbottle logs deploy/order-service | grep -i "Connected to DynamoDB" || \
  kubectl -n ticketbottle logs deploy/order-service | tail -20
```
Expected: all app pods `Running`/`Ready`; migrate Jobs `Completed`; order-service logs `Connected to DynamoDB!` with **no** credential errors (proves the instance profile works). If you see `NoCredentialProviders`/`AccessDenied`, re-check Task 4b (creds must be absent) and the instance-profile policy.

- [ ] **Step 4: Verify real DynamoDB is reachable from the pod (not local)**
```bash
kubectl -n ticketbottle exec deploy/order-service -- sh -c 'echo checking' >/dev/null 2>&1 || true
aws dynamodb describe-table --table-name ticketbottle-orders --region us-east-1 \
  --query "Table.ItemCount" --output text
```
Expected: table reachable (item count is fine at 0 pre-flow).

- [ ] **Step 5: No commit** (operational). Record: stack up on k3s from ECR.

---

## Task 6: Gate 2a — full purchase flow on k3s

Adapt the Gate-1 script to run against the tunneled gateway, then run the full flow to `order COMPLETED`.

**Files:**
- Modify: `deploy/scripts/gate1-purchase-flow.sh` (parametrize the gateway URL)

**Interfaces:**
- Consumes: the tunnel (`localhost:3000` → node NodePort 30000), `KUBECONFIG=/tmp/k3s.yaml`.
- Produces: a green end-to-end purchase flow on k3s.

- [ ] **Step 1: Parametrize the gateway URL in `deploy/scripts/gate1-purchase-flow.sh`**
Replace:
```bash
GW=http://localhost:3000/api
```
with:
```bash
GW=${GW:-http://localhost:3000/api}
```
(The gate's `kubectl ... exec statefulset/postgres` seeds already work against any cluster via the active `KUBECONFIG`.)

- [ ] **Step 2: Run the flow against k3s (tunnel + remote kubeconfig active)**
```bash
export KUBECONFIG=/tmp/k3s.yaml
GW=http://localhost:3000/api ./deploy/scripts/gate1-purchase-flow.sh
```
Expected: the script walks register → event → publish → seed ticket class → waitroom → order → webhook → poll, and prints a success line with `order ... COMPLETED`. The `payment-webhook` Service receives the simulated completion; `outbox-relay` publishes; `order-consumer` confirms.

- [ ] **Step 3: Confirm the order persisted in REAL DynamoDB**
```bash
aws dynamodb scan --table-name ticketbottle-orders --region us-east-1 \
  --select COUNT --query "Count" --output text
```
Expected: `>= 1` — proving the order was written to real DynamoDB via the instance profile (not a local pod).

- [ ] **Step 4: Commit**
```bash
git add deploy/scripts/gate1-purchase-flow.sh
git commit -m "feat(aws-phaseA): parametrize gate script GW for k3s (Gate 2a green)"
```

**Gate 2a is green when:** the purchase flow completes on k3s and the order is present in the real `ticketbottle-orders` table.

---

## Task 7: `make stop`/`make start` + Gate 2b (data survives stop/start)

**Files:**
- Modify: `deploy/Makefile` (add `k3s-*`, `stop`, `start`, `tunnel` targets)

**Interfaces:**
- Consumes: `envs/k3s` terraform outputs, `aws ec2` stop/start.
- Produces: the daily on/off switch; proof that EBS-backed PVCs survive a stop/start.

- [ ] **Step 1: Add k3s/stop/start targets to `deploy/Makefile`**
Append:
```makefile
# --- Rung 2: k3s on EC2 -------------------------------------------------------
K3S_ENV := terraform/envs/k3s
KUBECONFIG_FILE := /tmp/k3s.yaml

.PHONY: k3s-ip k3s-instance stop start tunnel k3s-kubeconfig k3s-deploy k3s-gate2

k3s-ip:            ## Print the current public IP of the k3s box
	@cd $(K3S_ENV) && terraform output -raw public_ip

k3s-instance:      ## Print the instance id
	@cd $(K3S_ENV) && terraform output -raw instance_id

stop:              ## Stop the k3s EC2 (halts all compute; EBS + data survive)
	aws ec2 stop-instances --instance-ids $$(cd $(K3S_ENV) && terraform output -raw instance_id)
	@echo "stopping — billing for compute halts once 'stopped'"

start:             ## Start the k3s EC2 and print the new SSH tunnel command
	aws ec2 start-instances --instance-ids $$(cd $(K3S_ENV) && terraform output -raw instance_id)
	aws ec2 wait instance-running --instance-ids $$(cd $(K3S_ENV) && terraform output -raw instance_id)
	@echo "started. Refresh outputs + open the tunnel:"
	@cd $(K3S_ENV) && terraform refresh >/dev/null && terraform output -raw ssh_command; echo

k3s-kubeconfig:    ## Fetch the k3s kubeconfig over SSH into $(KUBECONFIG_FILE)
	scp -o StrictHostKeyChecking=accept-new \
	  ec2-user@$$(cd $(K3S_ENV) && terraform output -raw public_ip):/etc/rancher/k3s/k3s.yaml $(KUBECONFIG_FILE)
	@echo "export KUBECONFIG=$(KUBECONFIG_FILE)"

k3s-gate2:         ## Run the purchase flow against k3s (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate1-purchase-flow.sh
```
Note: `start` re-assigns an **ephemeral** public IP (it changes); `terraform refresh` updates the outputs so `ssh_command`/`k3s-ip` are current. The kubeconfig (127.0.0.1) is unaffected.

- [ ] **Step 2: Baseline — capture current order count**
```bash
BEFORE=$(aws dynamodb scan --table-name ticketbottle-orders --region us-east-1 --select COUNT --query Count --output text)
echo "orders before: $BEFORE"
```

- [ ] **Step 3: Stop, wait, start**
```bash
make -C deploy stop
aws ec2 wait instance-stopped --instance-ids $(cd deploy/terraform/envs/k3s && terraform output -raw instance_id)
make -C deploy start          # prints the new ssh_command
```
Expected: instance transitions running→stopped→running; `start` prints a tunnel command with a (likely new) IP.

- [ ] **Step 4: Re-open the tunnel with the new IP, wait for k3s, re-check data**
```bash
eval "$(cd deploy/terraform/envs/k3s && terraform output -raw ssh_command)"   # new terminal; keep open
# in another terminal:
export KUBECONFIG=/tmp/k3s.yaml
kubectl -n ticketbottle get pods            # k3s + pods come back on their PVCs
AFTER=$(aws dynamodb scan --table-name ticketbottle-orders --region us-east-1 --select COUNT --query Count --output text)
echo "orders after: $AFTER"
```
Expected: pods return to Ready (Postgres/Redpanda/Temporal recover from EBS-backed PVCs); `AFTER == BEFORE` (DynamoDB is off-box, always durable). The **PVC-survival** proof is that Postgres/Redpanda pods rebind existing PVCs and the app comes back without re-migration errors.

- [ ] **Step 5: Re-run the flow post-restart (proves the restarted stack still works)**
```bash
make -C deploy k3s-gate2
```
Expected: another `order ... COMPLETED`; order count increments in DynamoDB.

- [ ] **Step 6: Commit**
```bash
git add deploy/Makefile
git commit -m "feat(aws-phaseA): make stop/start + k3s targets (Gate 2b: data survives stop/start)"
```

**Gate 2b is green when:** a full stop→start cycle returns the cluster to Ready on its existing PVCs, prior data is intact, and the purchase flow passes again.

---

## Task 8: Reclaim Mac disk + Phase A wrap-up

With k3s-on-EC2 proven, retire the local kind cluster as the daily driver and reclaim ~40 GB (spec B.7 Phase A).

**Files:** none.

- [ ] **Step 1: Stop the box (end the session cleanly first)**
```bash
make -C deploy stop
```

- [ ] **Step 2: Delete the local kind cluster + prune Docker**
```bash
kind delete cluster --name ticketbottle
docker system prune -a --volumes   # reclaims the ~37 GB of kind volumes + images
docker system df                    # confirm reclaimed
```
Note: this destroys **local** Postgres/Redpanda state only — it is fully reproducible via `make cluster-up`/`infra-up`/`apps-up` (kind remains the documented `$0` offline fallback, spec B.3a). The real stack now lives on the EC2.

- [ ] **Step 3: Verify Mac disk reclaimed**
```bash
df -h /
```
Expected: materially more free space than the ~16 GB baseline.

- [ ] **Step 4: Final cost sanity**
```bash
make -C deploy k3s-instance
aws ec2 describe-instances --instance-ids $(cd deploy/terraform/envs/k3s && terraform output -raw instance_id) \
  --query "Reservations[0].Instances[0].State.Name" --output text
```
Expected: `stopped` (no compute billing while idle; only the ~50 GB EBS ≈ $4/mo persists).

**Gate 2 (Phase A) is complete when:** the purchase flow is green on k3s (2a), a stop/start preserves data and the flow passes again (2b), `make stop`/`make start` work, and the Mac's kind cluster is deleted with disk reclaimed. **Next: Phase B (EKS → Gate 3).**

---

## Self-Review

**Spec coverage (Appendix B.7 Phase A + A.3/A.4):**
- `ec2-k3s` module + `values-k3s.yaml` → Tasks 2, 4. ✅
- gRPC health probes → **already present** as TCP readiness in `_appservice.tpl` (verified); proper gRPC health check deferred to Phase C as hardening — noted, not a gap. ✅
- Deploy from ECR → Task 5 (regcred via instance profile; Task 2 user-data). ✅
- Gate 2 (flow green + stop/start preserves data) → Tasks 6, 7. ✅
- `make stop/start` → Task 7. ✅
- Delete local kind, reclaim ~40 GB → Task 8. ✅
- t3.large on-demand / 50 GB / ephemeral IP (A.3/A.4) → Global Constraints + Task 2. ✅
- Instance-profile DynamoDB, no static keys (A.6/B) → Task 4b + Task 2 IAM. ✅
- S3 durable state before billable compute → Task 1. ✅

**Placeholder scan:** all HCL/YAML/bash complete; `REPLACE_WITH_*` appear only in the `.example` tfvars (intended user substitution, with the commands to compute them in Task 2 Step 9). No TBD/TODO. ✅

**Type/name consistency:** `tb.image` helper param `repo` matches every include's `"image"` value; `image.{registry,tag,pullPolicy}` used identically in `_appservice.tpl`, `payment-events.yaml`, `outbox-relay.yaml`, `migrations.yaml`; `state_bucket`/`ecr_registry`/`ssh_command`/`instance_id`/`public_ip` outputs match their consumers (Makefile, Task 3/7); `dynamodb_table_arn` from foundation remote state feeds the instance-profile policy. `order.awsAccessKeyId` empty-string gate in `config.yaml` matches the `values-k3s` empty value and the verified SDK behaviour in `pkg/dynamodb/client.go`. ✅

**Resolved from Phase 0's open item:** the payment webhook topology off-LocalStack is the in-cluster `payment-webhook` (payment-events image) + `outbox-relay`, deployed by `values-k3s` and driven by the gate's simulated webhook — identical to Gate 1. No Lambda needed on k3s.
