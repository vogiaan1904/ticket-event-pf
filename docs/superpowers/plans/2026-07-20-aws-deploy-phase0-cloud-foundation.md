# AWS Deploy Phase 0 — Cloud Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the shared, near-free AWS substrate the TicketBottle deploy depends on — cost guardrails, a minimal VPC, ECR repositories, the real DynamoDB orders table, GitHub-OIDC CI identity — and a GitHub Actions pipeline that builds every service image and pushes it to ECR. Nothing billable-heavy runs yet; this is the scaffolding Phase A (k3s on EC2) and Phase B (EKS) both consume.

**Architecture:** All infra is Terraform under `deploy/terraform/`. A single **persistent** root module `envs/foundation/` instantiates five small modules (`budget`, `vpc`, `ecr`, `dynamodb`, `iam-ci`) — this state is applied once and rarely destroyed, kept separate from the ephemeral `envs/k3s` (Phase A) and `envs/eks` (Phase B) roots so tearing those down never touches the foundation. Image builds move entirely into GitHub Actions (`.github/workflows/build-push-ecr.yml`): push → CI builds `linux/amd64` images → pushes to ECR via a short-lived OIDC-assumed role (no long-lived keys). The developer Mac and the future EC2 never build images.

**Tech Stack:** Terraform ≥ 1.5, AWS provider `~> 5.0`, AWS (Budgets, Cost Anomaly Detection, VPC, ECR, DynamoDB, IAM/OIDC), GitHub Actions, Docker Buildx, `aws` CLI v2.

## Global Constraints

- **Region:** `us-east-1` for every resource (cheapest; single-region by design).
- **Cost ceiling:** hard **$20/mo** target, **$40/mo** panic. The `budget` module is applied **before any other resource** and is a blocking gate.
- **Terraform state:** **local state** for the foundation (all Phase-0 resources are ~$0, so state loss is low-severity and recoverable via console). The **S3 remote backend is introduced in Phase A**, before the first billable compute. Do **not** commit `*.tfstate` or `.terraform/`.
- **No long-lived cloud keys in CI:** GitHub Actions authenticates via **OIDC → IAM role** only.
- **No NAT Gateway** anywhere (saves ~$32/mo): public subnets + tight security groups (SGs are Phase A).
- **DynamoDB billing mode:** `PAY_PER_REQUEST` (on-demand; ~free at idle).
- **Image platform:** all images built `--platform linux/amd64` (k3s/EKS nodes are amd64).
- **GitHub repo:** `vogiaan1904/ticket-event-pf`. **Account/region are never hard-coded** in HCL — use `data.aws_caller_identity`/`data.aws_region`.
- **Naming:** every resource prefixed `ticketbottle-` (or `tb-`) and tagged `{ Project = "ticketbottle", ManagedBy = "terraform", Env = "foundation" }` so Phase A/B can look them up by tag.
- **Terraform credentials:** Task 0 Step 5b makes `[default]` a `credential_process` profile bridging the `aws login` session to the AWS Go SDK, so every `terraform` command below runs with **no profile prefix**. Skip that step and Terraform reports `No valid credential sources found`.
- **Verification model:** this is infrastructure, not unit-testable code. Each task's "test" is `terraform plan`/`apply` succeeding plus an `aws` CLI assertion on the created resource — consistent with the spec's behavioural-gate philosophy (`docs/superpowers/specs/2026-07-09-aws-affordable-deployment-ladder-design.md`, §7). Do **not** invent unit tests for Terraform.

---

## File Structure

```
deploy/terraform/
  .gitignore                       # *.tfstate*, .terraform/, *.tfvars (except example)
  modules/
    budget/     { main.tf, variables.tf, outputs.tf }   # AWS Budgets + Cost Anomaly Detection
    vpc/        { main.tf, variables.tf, outputs.tf }   # VPC, 2 public subnets/2 AZs, IGW, no NAT
    ecr/        { main.tf, variables.tf, outputs.tf }   # one repo per image + lifecycle policy
    dynamodb/   { main.tf, variables.tf, outputs.tf }   # ticketbottle-orders + GSI1/GSI2
    iam-ci/     { main.tf, variables.tf, outputs.tf }   # GitHub OIDC provider + ECR-push role
  envs/
    foundation/
      main.tf                      # provider + module wiring
      variables.tf
      outputs.tf
      terraform.tfvars.example     # committed template (no secrets)
      # terraform.tfvars           # real values — GITIGNORED
.github/workflows/
  build-push-ecr.yml               # build every image -> push to ECR (OIDC)
docs/superpowers/plans/
  2026-07-20-aws-deploy-phase0-cloud-foundation.md   # this plan
```

Each module has one responsibility and is consumed by name from `envs/foundation/main.tf`. Phase A's `envs/k3s` will look these up by tag/name (VPC, subnets) and by output (ECR URLs, DynamoDB ARN) — so consistent tagging in Phase 0 is load-bearing.

---

## Task 0: Account prerequisites & local tooling (human runbook)

This task is **manual console/CLI work** — an agent cannot do it. Complete every checkbox and paste the verification output before any Terraform runs.

**Files:** none (account state + local machine).

**Interfaces:**
- Produces: a working default `aws` CLI identity in `us-east-1` with admin-equivalent permissions, used by Terraform in every later task; MFA-protected root; a confirmed budget-alert email address.

- [ ] **Step 1: Secure the root account**
  In the AWS Console (signed in as root): enable **MFA** on the root user. Then create the day-to-day identity — either an **IAM user** in a group with `AdministratorAccess`, or an **IAM Identity Center** user. Stop using root after this.

- [ ] **Step 2: Enable billing visibility**
  Console → Billing → *IAM user and role access to Billing Information* → **Activate**. Open **Cost Explorer** once (enabling it is a one-time, irreversible, free action) — Cost Anomaly Detection in Task 1 depends on it.

- [ ] **Step 3: Install local tooling**
  Run and confirm versions:
```bash
aws --version        # aws-cli/2.x
terraform -version   # Terraform v1.5+  ; provider resolved later
docker buildx version
```
  Expected: all three print versions. If missing: `brew install awscli terraform` and ensure Docker Desktop/buildx is present.

- [ ] **Step 4: Configure CLI credentials for the admin identity (us-east-1)**
  Use `aws login` (browser sign-in, **short-lived** session) rather than minting a long-lived `AKIA…` access key — consistent with this plan's no-static-keys posture (OIDC for CI, instance profile for the box).
```bash
aws login                          # browser sign-in; --region here applies to THIS command only
aws configure set region us-east-1 # the stored default is set separately
```
  Note: the session **expires**; on `ExpiredToken` mid-apply, re-run `aws login` and re-apply (Terraform is idempotent).

- [ ] **Step 5: Verify identity, region, and *credential source***
```bash
aws configure list          # TYPE column must read `login`, not `shared-credentials-file`/`env`
aws sts get-caller-identity
aws configure get region
```
  Expected: a JSON body with your `Account` (12 digits) and the admin user/role `Arn`; region prints `us-east-1`. **Record the Account ID** — used to sanity-check ECR URLs later.
  ⚠️ **Verify the Account matches the account you intend to build in.** The credential chain is first-match-wins, so a stale `~/.aws/credentials` from a previous account silently overrides `aws login` — you would build every resource in the wrong account. If `aws configure list` shows `shared-credentials-file`, remove/rename that file (and delete the key in its own account's IAM) before proceeding.

- [ ] **Step 5b: Bridge the login session to Terraform (`credential_process`)**
  `aws login` is an AWS **CLI** feature; Terraform uses the AWS **Go SDK**, which cannot read `login_session`/`~/.aws/login/cache/`. Without this step `terraform plan` fails with `No valid credential sources found` + an IMDS `169.254.169.254 ... host is down` (the SDK falling through to its last resort, which only exists on EC2).
  Make **`[default]` the bridge** so no command needs an `AWS_PROFILE` prefix. Replace `~/.aws/config` with:
```ini
[default]
region = us-east-1
credential_process = aws configure export-credentials --profile awslogin

[profile awslogin]
region = us-east-1
login_session = arn:aws:iam::<ACCOUNT>:user/admin      # written by `aws login`
```
```bash
aws sts get-caller-identity     # must match Step 5's Account, with no prefix
```
  Expected: identical Account/Arn to Step 5, and every `terraform`/SDK tool now resolves credentials transparently. No recursion occurs because `[profile awslogin]` has no `credential_process` of its own.
  ⚠️ Re-login (on `ExpiredToken`) is now `aws login --profile awslogin` — the profile that holds the session.

- [ ] **Step 6: Decide the budget-alert email**
  Choose the email AWS Budgets + Anomaly Detection will notify (your `vogiaan1904@gmail.com` unless you prefer another). You will confirm the Anomaly Detection subscription email after Task 1's apply.

- [ ] **Step 7: Note the credit expiry date**
  Console → Billing → **Credits**. Record the expiry date of the $200 promotional credits in your runbook — that date is when "effectively free" ends (spec Appendix A.5).

**Gate:** `aws sts get-caller-identity` returns the admin ARN in the intended account, region is `us-east-1`, root has MFA, Cost Explorer is enabled. Do not proceed otherwise.

---

## Task 1: Terraform skeleton + budget guardrails (apply FIRST)

The single most important safeguard. Provision AWS Budgets + Cost Anomaly Detection **before** anything else, so every later resource is created under an active alarm.

**Files:**
- Create: `deploy/terraform/.gitignore`
- Create: `deploy/terraform/modules/budget/{main.tf,variables.tf,outputs.tf}`
- Create: `deploy/terraform/envs/foundation/{main.tf,variables.tf,outputs.tf,terraform.tfvars.example}`
- Create (gitignored, local): `deploy/terraform/envs/foundation/terraform.tfvars`

**Interfaces:**
- Consumes: Task 0's local AWS identity.
- Produces: an applied `budget` module; the `envs/foundation` root that Tasks 2–5 extend; variables `aws_region` (string), `alert_email` (string), `github_repo` (string, `"owner/name"`), `monthly_budget_usd` (number, default `40`).

- [ ] **Step 1: Create the Terraform gitignore**

`deploy/terraform/.gitignore`:
```gitignore
# Terraform
**/.terraform/*
*.tfstate
*.tfstate.*
crash.log
crash.*.log
*.tfvars
!*.tfvars.example
override.tf
override.tf.json
*_override.tf
*_override.tf.json
.terraform.lock.hcl
```

- [ ] **Step 2: Write the budget module — `deploy/terraform/modules/budget/variables.tf`**
```hcl
variable "alert_email" {
  type        = string
  description = "Email address to receive budget + anomaly alerts."
}

variable "monthly_budget_usd" {
  type        = number
  description = "Hard monthly panic ceiling in USD. Notifications fire at 50% and 100%."
  default     = 40
}

variable "tags" {
  type    = map(string)
  default = {}
}
```

- [ ] **Step 3: Write the budget module — `deploy/terraform/modules/budget/main.tf`**
```hcl
# A single monthly cost budget set to the PANIC ceiling ($40), with notifications
# at 50% ($20 = the real target) and 100% ($40), plus a forecast alarm. One budget
# keeps us inside the AWS Budgets free tier (first 2 budgets free).
resource "aws_budgets_budget" "monthly" {
  name         = "ticketbottle-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  # Actual spend crosses the $20 target.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 50
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }

  # Actual spend hits the $40 panic ceiling.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }

  # Forecast says we will blow the ceiling this month.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alert_email]
  }
}

# Cost Anomaly Detection: catches a forgotten resource that spikes cost between
# budget cycles. Monitors per-AWS-service spend, emails on any anomaly >= $5.
resource "aws_ce_anomaly_monitor" "service" {
  name              = "ticketbottle-service-monitor"
  monitor_type      = "DIMENSIONAL"
  monitor_dimension = "SERVICE"
  tags              = var.tags
}

resource "aws_ce_anomaly_subscription" "alerts" {
  name      = "ticketbottle-anomaly-alerts"
  frequency = "DAILY"

  monitor_arn_list = [aws_ce_anomaly_monitor.service.arn]

  subscriber {
    type    = "EMAIL"
    address = var.alert_email
  }

  threshold_expression {
    dimension {
      key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
      match_options = ["GREATER_THAN_OR_EQUAL"]
      values        = ["5"]
    }
  }

  tags = var.tags
}
```

- [ ] **Step 4: Write the budget module outputs — `deploy/terraform/modules/budget/outputs.tf`**
```hcl
output "budget_name" {
  value = aws_budgets_budget.monthly.name
}

output "anomaly_monitor_arn" {
  value = aws_ce_anomaly_monitor.service.arn
}
```

- [ ] **Step 5: Write the foundation root variables — `deploy/terraform/envs/foundation/variables.tf`**
```hcl
variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "alert_email" {
  type        = string
  description = "Budget + anomaly alert email."
}

variable "github_repo" {
  type        = string
  description = "GitHub repo allowed to assume the CI role, as owner/name."
  default     = "vogiaan1904/ticket-event-pf"
}

variable "monthly_budget_usd" {
  type    = number
  default = 40
}
```

- [ ] **Step 6: Write the foundation root — `deploy/terraform/envs/foundation/main.tf` (budget only for now)**
```hcl
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
  # Phase 0 uses local state (all resources are ~$0). Phase A introduces S3.
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = {
      Project   = "ticketbottle"
      ManagedBy = "terraform"
      Env       = "foundation"
    }
  }
}

locals {
  tags = {
    Project   = "ticketbottle"
    ManagedBy = "terraform"
    Env       = "foundation"
  }
}

module "budget" {
  source             = "../../modules/budget"
  alert_email        = var.alert_email
  monthly_budget_usd = var.monthly_budget_usd
  tags               = local.tags
}
```

- [ ] **Step 7: Write the foundation outputs stub — `deploy/terraform/envs/foundation/outputs.tf`**
```hcl
output "budget_name" {
  value = module.budget.budget_name
}
```

- [ ] **Step 8: Write the tfvars template — `deploy/terraform/envs/foundation/terraform.tfvars.example`**
```hcl
aws_region         = "us-east-1"
alert_email        = "you@example.com"
github_repo        = "vogiaan1904/ticket-event-pf"
monthly_budget_usd = 40
```

- [ ] **Step 9: Create the real (gitignored) tfvars**
```bash
cd deploy/terraform/envs/foundation
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars: set alert_email to your real address
```

- [ ] **Step 10: Init, validate, and plan**
```bash
cd deploy/terraform/envs/foundation
terraform init
terraform fmt -recursive ../..
terraform validate
terraform plan
```
Expected: `Success! The configuration is valid.`; plan shows **3 to add** (`aws_budgets_budget.monthly`, `aws_ce_anomaly_monitor.service`, `aws_ce_anomaly_subscription.alerts`), 0 to change, 0 to destroy.

- [ ] **Step 11: Apply**
```bash
terraform apply
# type 'yes'
```
Expected: `Apply complete! Resources: 3 added.`

  ⚠️ **Likely failure: `ValidationException: Limit exceeded on dimensional spend monitor creation`.** AWS permits exactly **one** `DIMENSIONAL`/`SERVICE` anomaly monitor per account, and launching Cost Explorer (Task 0 Step 3) auto-creates one named `Default-Services-Monitor`. Import it instead of creating a second:
```bash
aws ce get-anomaly-monitors --query 'AnomalyMonitors[?MonitorType==`DIMENSIONAL`].MonitorArn' --output text
terraform import module.budget.aws_ce_anomaly_monitor.service "<that ARN>"
terraform plan    # MUST read `will be updated in-place` (a rename), NOT `must be replaced`
terraform apply
```
  If the plan says *must be replaced*, stop — applying would destroy the monitor and then fail to re-create it against the same limit. Align `name` in `modules/budget/main.tf` to the existing monitor's name instead.
  Note: the monitor is now managed here, so `terraform destroy` on `foundation` removes the account's anomaly detection.

  ⚠️ **Budget count:** AWS gives **2 budgets free**, ~$0.02/day each beyond that. If you created budgets by hand before this, delete the redundant ones — `ticketbottle-monthly` supersedes a plain $20 budget because it alerts at 50%/100%/forecast rather than one threshold.
```bash
aws budgets describe-budgets --account-id "$(aws sts get-caller-identity --query Account --output text)" \
  --query 'Budgets[].BudgetName' --output text
```

- [ ] **Step 12: Confirm the anomaly-subscription email**
Check the inbox for `var.alert_email` and **confirm** the AWS Cost Anomaly Detection subscription (AWS sends a confirmation email). Budgets notifications need no confirmation.

- [ ] **Step 13: Verify via CLI**
```bash
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
aws budgets describe-budgets --account-id "$ACCOUNT" \
  --query "Budgets[?BudgetName=='ticketbottle-monthly'].[BudgetName,BudgetLimit.Amount]" --output text
aws ce get-anomaly-monitors --query "AnomalyMonitors[].MonitorName" --output text
```
Expected: prints `ticketbottle-monthly  40` and `ticketbottle-service-monitor`.

- [ ] **Step 14: Commit**
```bash
git add deploy/terraform/.gitignore deploy/terraform/modules/budget deploy/terraform/envs/foundation
git commit -m "feat(aws-phase0): terraform skeleton + budget + cost-anomaly guardrails"
```

---

## Task 2: VPC module (2 public subnets, no NAT)

A minimal network for Phase A's EC2 and Phase B's EKS. Two public subnets across two AZs (EKS/ALB require ≥2 AZs); an internet gateway; a public route table. **No NAT** (cost). Security groups are deferred to the compute phases.

**Files:**
- Create: `deploy/terraform/modules/vpc/{main.tf,variables.tf,outputs.tf}`
- Modify: `deploy/terraform/envs/foundation/main.tf` (add module + data source)
- Modify: `deploy/terraform/envs/foundation/outputs.tf`

**Interfaces:**
- Consumes: `local.tags`.
- Produces: outputs `vpc_id` (string), `public_subnet_ids` (list(string)) — consumed by Phase A/B via tag lookup and by the outputs block here.

- [ ] **Step 1: Write `deploy/terraform/modules/vpc/variables.tf`**
```hcl
variable "cidr_block" {
  type    = string
  default = "10.0.0.0/16"
}

variable "public_subnet_cidrs" {
  type    = list(string)
  default = ["10.0.1.0/24", "10.0.2.0/24"]
}

variable "tags" {
  type    = map(string)
  default = {}
}
```

- [ ] **Step 2: Write `deploy/terraform/modules/vpc/main.tf`**
```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = var.cidr_block
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = merge(var.tags, { Name = "ticketbottle-vpc" })
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = merge(var.tags, { Name = "ticketbottle-igw" })
}

resource "aws_subnet" "public" {
  count                   = length(var.public_subnet_cidrs)
  vpc_id                  = aws_vpc.this.id
  cidr_block              = var.public_subnet_cidrs[count.index]
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = true
  tags = merge(var.tags, {
    Name                     = "ticketbottle-public-${count.index}"
    "kubernetes.io/role/elb" = "1" # lets the EKS ALB controller (Phase B) discover public subnets
  })
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = merge(var.tags, { Name = "ticketbottle-public-rt" })
}

resource "aws_route_table_association" "public" {
  count          = length(aws_subnet.public)
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}
```

- [ ] **Step 3: Write `deploy/terraform/modules/vpc/outputs.tf`**
```hcl
output "vpc_id" {
  value = aws_vpc.this.id
}

output "public_subnet_ids" {
  value = aws_subnet.public[*].id
}
```

- [ ] **Step 4: Wire the module into `deploy/terraform/envs/foundation/main.tf`**
Append:
```hcl
module "vpc" {
  source = "../../modules/vpc"
  tags   = local.tags
}
```

- [ ] **Step 5: Add outputs to `deploy/terraform/envs/foundation/outputs.tf`**
Append:
```hcl
output "vpc_id" {
  value = module.vpc.vpc_id
}

output "public_subnet_ids" {
  value = module.vpc.public_subnet_ids
}
```

- [ ] **Step 6: Plan**
```bash
cd deploy/terraform/envs/foundation
terraform fmt -recursive ../.. && terraform validate && terraform plan
```
Expected: valid; plan shows **7 to add** (vpc, igw, 2 subnets, route table, 2 associations), 0 to change/destroy.

- [ ] **Step 7: Apply**
```bash
terraform apply   # yes
```
Expected: `Apply complete! Resources: 7 added.`

- [ ] **Step 8: Verify**
```bash
terraform output vpc_id
aws ec2 describe-subnets \
  --filters "Name=tag:Project,Values=ticketbottle" \
  --query "Subnets[].[SubnetId,AvailabilityZone,MapPublicIpOnLaunch]" --output table
```
Expected: two subnets in two different AZs, `MapPublicIpOnLaunch = True`.

- [ ] **Step 9: Commit**
```bash
git add deploy/terraform/modules/vpc deploy/terraform/envs/foundation
git commit -m "feat(aws-phase0): minimal VPC with two public subnets, no NAT"
```

---

## Task 3: ECR module (one repo per image + lifecycle policy)

A repository per service image, each with a lifecycle policy so old images don't accrue storage cost.

**Files:**
- Create: `deploy/terraform/modules/ecr/{main.tf,variables.tf,outputs.tf}`
- Modify: `deploy/terraform/envs/foundation/main.tf`
- Modify: `deploy/terraform/envs/foundation/outputs.tf`

**Interfaces:**
- Consumes: `local.tags`.
- Produces: outputs `repository_urls` (map(string): image-name → repo URL) and `repository_names` (list(string)) — consumed by the CI workflow (Task 6) and Phase A `values-k3s.yaml`.

- [ ] **Step 1: Write `deploy/terraform/modules/ecr/variables.tf`**
```hcl
variable "image_names" {
  type        = list(string)
  description = "ECR repo names (without registry host), e.g. ticketbottle/user."
}

variable "tags" {
  type    = map(string)
  default = {}
}
```

- [ ] **Step 2: Write `deploy/terraform/modules/ecr/main.tf`**
```hcl
resource "aws_ecr_repository" "this" {
  for_each             = toset(var.image_names)
  name                 = each.value
  image_tag_mutability = "MUTABLE" # we push :latest + :<sha>
  image_scanning_configuration {
    scan_on_push = true
  }
  tags = var.tags
}

# Keep at most 10 tagged images, and expire untagged images after 3 days.
resource "aws_ecr_lifecycle_policy" "this" {
  for_each   = aws_ecr_repository.this
  repository = each.value.name
  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images older than 3 days"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 3
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the last 10 tagged images"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["latest", "sha-"]
          countType     = "imageCountMoreThan"
          countNumber   = 10
        }
        action = { type = "expire" }
      }
    ]
  })
}
```

- [ ] **Step 3: Write `deploy/terraform/modules/ecr/outputs.tf`**
```hcl
output "repository_urls" {
  value = { for name, repo in aws_ecr_repository.this : name => repo.repository_url }
}

output "repository_names" {
  value = [for repo in aws_ecr_repository.this : repo.name]
}
```

- [ ] **Step 4: Add the image list + module to `deploy/terraform/envs/foundation/main.tf`**
Append (the list is the authoritative CI/deploy image inventory — mirrors `deploy/scripts/build-images.sh` + `build-migrate-images.sh`; `payment-events` is the legacy Phase-0B adapter, kept available but likely retired in Phase A):
```hcl
locals {
  image_names = [
    "ticketbottle/user",
    "ticketbottle/event",
    "ticketbottle/payment",
    "ticketbottle/inventory",
    "ticketbottle/waitroom",
    "ticketbottle/order-api",
    "ticketbottle/order-consumer",
    "ticketbottle/gateway",
    "ticketbottle/outbox-relay",
    "ticketbottle/user-migrate",
    "ticketbottle/event-migrate",
    "ticketbottle/payment-migrate",
    "ticketbottle/payment-events",
  ]
}

module "ecr" {
  source      = "../../modules/ecr"
  image_names = local.image_names
  tags        = local.tags
}
```

- [ ] **Step 5: Add outputs to `deploy/terraform/envs/foundation/outputs.tf`**
Append:
```hcl
output "ecr_repository_urls" {
  value = module.ecr.repository_urls
}
```

- [ ] **Step 6: Plan**
```bash
cd deploy/terraform/envs/foundation
terraform fmt -recursive ../.. && terraform validate && terraform plan
```
Expected: valid; plan shows **26 to add** (13 repositories + 13 lifecycle policies).

- [ ] **Step 7: Apply**
```bash
terraform apply   # yes
```
Expected: `Apply complete! Resources: 26 added.`

- [ ] **Step 8: Verify**
```bash
aws ecr describe-repositories \
  --query "sort_by(repositories,&repositoryName)[].repositoryName" --output text
terraform output ecr_repository_urls
```
Expected: all 13 `ticketbottle/*` repos listed; the output map shows `<account>.dkr.ecr.us-east-1.amazonaws.com/ticketbottle/<name>` URLs.

- [ ] **Step 9: Commit**
```bash
git add deploy/terraform/modules/ecr deploy/terraform/envs/foundation
git commit -m "feat(aws-phase0): ECR repositories per image with lifecycle policies"
```

---

## Task 4: DynamoDB module (real orders table)

The real `ticketbottle-orders` table, schema-identical to `services/order-svc/scripts/init-dynamodb.sh` (PK/SK + GSI1 + GSI2, projection ALL) but **on-demand** billing instead of the LocalStack provisioned 5/5.

**Files:**
- Create: `deploy/terraform/modules/dynamodb/{main.tf,variables.tf,outputs.tf}`
- Modify: `deploy/terraform/envs/foundation/main.tf`
- Modify: `deploy/terraform/envs/foundation/outputs.tf`

**Interfaces:**
- Consumes: `local.tags`.
- Produces: outputs `table_name` (string, `ticketbottle-orders`) and `table_arn` (string) — consumed by Phase A instance-profile IAM (DynamoDB access) and order-svc config.

- [ ] **Step 1: Write `deploy/terraform/modules/dynamodb/variables.tf`**
```hcl
variable "table_name" {
  type    = string
  default = "ticketbottle-orders"
}

variable "tags" {
  type    = map(string)
  default = {}
}
```

- [ ] **Step 2: Write `deploy/terraform/modules/dynamodb/main.tf`**
```hcl
resource "aws_dynamodb_table" "orders" {
  name         = var.table_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }
  attribute {
    name = "SK"
    type = "S"
  }
  attribute {
    name = "GSI1PK"
    type = "S"
  }
  attribute {
    name = "GSI1SK"
    type = "S"
  }
  attribute {
    name = "GSI2PK"
    type = "S"
  }
  attribute {
    name = "GSI2SK"
    type = "S"
  }

  global_secondary_index {
    name            = "GSI1"
    hash_key        = "GSI1PK"
    range_key       = "GSI1SK"
    projection_type = "ALL"
  }
  global_secondary_index {
    name            = "GSI2"
    hash_key        = "GSI2PK"
    range_key       = "GSI2SK"
    projection_type = "ALL"
  }

  tags = merge(var.tags, { Name = var.table_name })
}
```

- [ ] **Step 3: Write `deploy/terraform/modules/dynamodb/outputs.tf`**
```hcl
output "table_name" {
  value = aws_dynamodb_table.orders.name
}

output "table_arn" {
  value = aws_dynamodb_table.orders.arn
}
```

- [ ] **Step 4: Wire module into `deploy/terraform/envs/foundation/main.tf`**
Append:
```hcl
module "dynamodb" {
  source = "../../modules/dynamodb"
  tags   = local.tags
}
```

- [ ] **Step 5: Add outputs to `deploy/terraform/envs/foundation/outputs.tf`**
Append:
```hcl
output "dynamodb_table_name" {
  value = module.dynamodb.table_name
}

output "dynamodb_table_arn" {
  value = module.dynamodb.table_arn
}
```

- [ ] **Step 6: Plan**
```bash
cd deploy/terraform/envs/foundation
terraform fmt -recursive ../.. && terraform validate && terraform plan
```
Expected: valid; plan shows **1 to add** (`aws_dynamodb_table.orders`).

- [ ] **Step 7: Apply**
```bash
terraform apply   # yes
```
Expected: `Apply complete! Resources: 1 added.`

- [ ] **Step 8: Verify (table ACTIVE + both GSIs)**
```bash
aws dynamodb describe-table --table-name ticketbottle-orders --region us-east-1 \
  --query "Table.[TableStatus,BillingModeSummary.BillingMode,length(GlobalSecondaryIndexes)]" --output text
```
Expected: `ACTIVE  PAY_PER_REQUEST  2`.

- [ ] **Step 9: Commit**
```bash
git add deploy/terraform/modules/dynamodb deploy/terraform/envs/foundation
git commit -m "feat(aws-phase0): real DynamoDB orders table (on-demand, GSI1/GSI2)"
```

---

## Task 5: IAM-CI module (GitHub OIDC provider + ECR-push role)

Lets GitHub Actions assume a short-lived AWS role via OIDC to push images — no static keys anywhere.

**Files:**
- Create: `deploy/terraform/modules/iam-ci/{main.tf,variables.tf,outputs.tf}`
- Modify: `deploy/terraform/envs/foundation/main.tf`
- Modify: `deploy/terraform/envs/foundation/outputs.tf`

**Interfaces:**
- Consumes: `var.github_repo`, `local.tags`.
- Produces: output `ci_role_arn` (string) — pasted into the GitHub Actions workflow (Task 6) as the `role-to-assume`.

- [ ] **Step 1: Write `deploy/terraform/modules/iam-ci/variables.tf`**
```hcl
variable "github_repo" {
  type        = string
  description = "owner/name of the repo allowed to assume this role."
}

variable "tags" {
  type    = map(string)
  default = {}
}
```

- [ ] **Step 2: Write `deploy/terraform/modules/iam-ci/main.tf`**
```hcl
# GitHub's OIDC identity provider. The thumbprint is fetched dynamically so it
# survives GitHub's cert rotations.
data "tls_certificate" "github" {
  url = "https://token.actions.githubusercontent.com/.well-known/openid-configuration"
}

resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.github.certificates[0].sha1_fingerprint]
  tags            = var.tags
}

# Role assumable only by workflows in this repo (any branch).
data "aws_iam_policy_document" "assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:${var.github_repo}:*"]
    }
  }
}

resource "aws_iam_role" "ci" {
  name               = "ticketbottle-github-actions-ecr"
  assume_role_policy = data.aws_iam_policy_document.assume.json
  tags               = var.tags
}

# ECR push/pull. GetAuthorizationToken must be on "*"; the rest scoped to our repos.
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

data "aws_iam_policy_document" "ecr_push" {
  statement {
    sid       = "EcrAuth"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }
  statement {
    sid    = "EcrPushPull"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:GetDownloadUrlForLayer",
      "ecr:BatchGetImage",
      "ecr:PutImage",
      "ecr:InitiateLayerUpload",
      "ecr:UploadLayerPart",
      "ecr:CompleteLayerUpload",
    ]
    resources = [
      "arn:aws:ecr:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:repository/ticketbottle/*"
    ]
  }
}

resource "aws_iam_role_policy" "ci_ecr" {
  name   = "ecr-push"
  role   = aws_iam_role.ci.id
  policy = data.aws_iam_policy_document.ecr_push.json
}
```

- [ ] **Step 3: Write `deploy/terraform/modules/iam-ci/outputs.tf`**
```hcl
output "ci_role_arn" {
  value = aws_iam_role.ci.arn
}
```

- [ ] **Step 4: Wire module into `deploy/terraform/envs/foundation/main.tf`**
Append:
```hcl
module "iam_ci" {
  source      = "../../modules/iam-ci"
  github_repo = var.github_repo
  tags        = local.tags
}
```

- [ ] **Step 5: Add output to `deploy/terraform/envs/foundation/outputs.tf`**
Append:
```hcl
output "ci_role_arn" {
  value = module.iam_ci.ci_role_arn
}
```

- [ ] **Step 6: Plan**
```bash
cd deploy/terraform/envs/foundation
terraform init  # picks up the new hashicorp/tls provider
terraform fmt -recursive ../.. && terraform validate && terraform plan
```
Expected: valid; plan shows **3 to add** — `aws_iam_openid_connect_provider.github`, `aws_iam_role.ci`, `aws_iam_role_policy.ci_ecr` (the `tls`/`aws_iam_policy_document`/caller-identity/region data sources are reads, not creates). `terraform init` also installs the `hashicorp/tls` provider.

- [ ] **Step 7: Apply**
```bash
terraform apply   # yes
```
Expected: `Apply complete!` with the OIDC provider, role, and policy added.

- [ ] **Step 8: Verify + capture the role ARN**
```bash
terraform output ci_role_arn
aws iam list-open-id-connect-providers --output text
```
Expected: role ARN like `arn:aws:iam::<account>:role/ticketbottle-github-actions-ecr`; an OIDC provider for `token.actions.githubusercontent.com`. **Record `ci_role_arn`** for Task 6.

- [ ] **Step 9: Commit**
```bash
git add deploy/terraform/modules/iam-ci deploy/terraform/envs/foundation
git commit -m "feat(aws-phase0): GitHub OIDC provider + ECR-push CI role"
```

---

## Task 6: CI pipeline — build every image, push to ECR

A GitHub Actions workflow that assumes the CI role via OIDC and builds+pushes all images to ECR. This is what makes the Mac and EC2 stop building images.

**Files:**
- Create: `.github/workflows/build-push-ecr.yml`

**Interfaces:**
- Consumes: `ci_role_arn` (Task 5 output, set as a repo variable), the ECR repos (Task 3).
- Produces: `:latest` and `:sha-<short>` tags in every `ticketbottle/*` runtime + migrate repo. Phase A's `values-k3s.yaml` references these by tag.

- [ ] **Step 1: Set the role ARN as a GitHub repo variable**
In the GitHub repo → Settings → Secrets and variables → Actions → **Variables** → New repository variable:
`AWS_CI_ROLE_ARN` = the `ci_role_arn` from Task 5. (A variable, not a secret — an ARN is not sensitive, and OIDC means there's no key to hide.)

- [ ] **Step 2: Write `.github/workflows/build-push-ecr.yml`**
```yaml
name: build-push-ecr

on:
  push:
    branches: [main]
    paths:
      - "services/**"
      - "deploy/adapters/**"
      - ".github/workflows/build-push-ecr.yml"
  workflow_dispatch: {}

concurrency:
  group: build-push-ecr-${{ github.ref }}
  cancel-in-progress: true

permissions:
  id-token: write   # required for OIDC
  contents: read

env:
  AWS_REGION: us-east-1

jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        include:
          # runtime images (repo, context, dockerfile, target)
          - { repo: ticketbottle/user,           context: services/user-svc,      dockerfile: services/user-svc/Dockerfile,             target: "" }
          - { repo: ticketbottle/event,          context: services/event-svc,     dockerfile: services/event-svc/Dockerfile,            target: "" }
          - { repo: ticketbottle/payment,        context: services/payment-svc,   dockerfile: services/payment-svc/Dockerfile,          target: "" }
          - { repo: ticketbottle/inventory,      context: services/inventory-svc, dockerfile: services/inventory-svc/Dockerfile,         target: "" }
          - { repo: ticketbottle/waitroom,       context: services/waitroom-svc,  dockerfile: services/waitroom-svc/Dockerfile,          target: "" }
          - { repo: ticketbottle/order-api,      context: services/order-svc,     dockerfile: services/order-svc/cmd/api/Dockerfile,     target: "" }
          - { repo: ticketbottle/order-consumer, context: services/order-svc,     dockerfile: services/order-svc/cmd/consumer/Dockerfile, target: "" }
          - { repo: ticketbottle/gateway,        context: services/api-gateway,   dockerfile: services/api-gateway/Dockerfile,           target: "" }
          - { repo: ticketbottle/outbox-relay,   context: services/payment-svc,   dockerfile: services/payment-svc/outbox-relay/Dockerfile, target: "" }
          - { repo: ticketbottle/payment-events, context: deploy/adapters/payment-events, dockerfile: deploy/adapters/payment-events/Dockerfile, target: "" }
          # prisma migrate images (builder stage of the same Dockerfile)
          - { repo: ticketbottle/user-migrate,    context: services/user-svc,     dockerfile: services/user-svc/Dockerfile,    target: builder }
          - { repo: ticketbottle/event-migrate,   context: services/event-svc,    dockerfile: services/event-svc/Dockerfile,   target: builder }
          - { repo: ticketbottle/payment-migrate, context: services/payment-svc,  dockerfile: services/payment-svc/Dockerfile, target: builder }
    steps:
      - uses: actions/checkout@v4

      - name: Configure AWS credentials (OIDC)
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_CI_ROLE_ARN }}
          aws-region: ${{ env.AWS_REGION }}

      - name: Login to Amazon ECR
        id: ecr
        uses: aws-actions/amazon-ecr-login@v2

      - name: Set up Buildx
        uses: docker/setup-buildx-action@v3

      - name: Build and push ${{ matrix.repo }}
        uses: docker/build-push-action@v6
        with:
          context: ${{ matrix.context }}
          file: ${{ matrix.dockerfile }}
          target: ${{ matrix.target }}
          platforms: linux/amd64
          push: true
          tags: |
            ${{ steps.ecr.outputs.registry }}/${{ matrix.repo }}:latest
            ${{ steps.ecr.outputs.registry }}/${{ matrix.repo }}:sha-${{ github.sha }}
          cache-from: type=gha,scope=${{ matrix.repo }}
          cache-to: type=gha,mode=max,scope=${{ matrix.repo }}
```

- [ ] **Step 3: Get the workflow onto the *default branch* (`main`)**
  ⚠️ **`workflow_dispatch`/`gh workflow run` only work if the workflow file exists on the repository's default branch.** A workflow that lives only on a feature branch is invisible to GitHub — `gh workflow list` is empty and `gh workflow run … --ref main` returns **404 Not Found** (this is *not* a private-repo/permissions problem; `gh auth status` will show `repo`+`workflow` scopes just fine). So the file must land on `main` at least once before it can be triggered.
```bash
# fast-forward the feature branch into main (main is a strict ancestor), then publish main:
git checkout main
git merge --ff-only <feature-branch>
git push origin main
```
  Because the pushed commits include `services/**`, this **also auto-triggers** the workflow via `on: push` — so Step 4 is often unnecessary; it's only needed for re-runs. (If you truly cannot merge yet, the minimal alternative is `git checkout main && git checkout <branch> -- .github/workflows/build-push-ecr.yml && git commit && git push origin main` — the workflow still must reach `main`.)

- [ ] **Step 4: Trigger (if not already auto-triggered) + watch the run**
```bash
gh workflow list                                 # the workflow now appears — was empty before Step 3
gh workflow run build-push-ecr.yml --ref main
gh run watch
```
Expected: the matrix runs 13 parallel jobs; all succeed. First run is slow (cold `gha` cache); the empty `target: ""` is a no-op for runtime images.
  ⚠️ **Next failure to expect (Task 5 trust policy):** once the 404 is gone, the *Configure AWS credentials (OIDC)* step fails if the CI role's trust `sub` condition doesn't match this ref. Have the role's trust policy open — matching `repo:<owner>/<repo>:ref:refs/heads/main` (or `:*`) against the ref you dispatched is the Lab 04 lesson.

- [ ] **Step 5: Verify images landed in ECR**
```bash
for r in user event payment inventory waitroom order-api order-consumer gateway outbox-relay user-migrate event-migrate payment-migrate payment-events; do
  echo -n "ticketbottle/$r: "
  aws ecr describe-images --repository-name "ticketbottle/$r" --region us-east-1 \
    --query "sort_by(imageDetails,&imagePushedAt)[-1].imageTags" --output text 2>/dev/null || echo "MISSING"
done
```
Expected: every repo prints tags including `latest` and a `sha-...` tag.

- [ ] **Step 6: Commit any workflow fixes**
If a job failed (e.g. a `.dockerignore` gap resurfacing per project memory `ts-svc-build-broken-node-modules`), fix and re-push:
```bash
git add .github/workflows/build-push-ecr.yml
git commit -m "fix(aws-phase0): CI image build corrections"
```

---

## Task 7: Gate 0 — foundation verification & handoff

A single checklist proving the foundation is complete and safe before Phase A spends money on compute.

**Files:** none (verification only).

- [ ] **Step 1: Guardrails live**
```bash
ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
aws budgets describe-budgets --account-id "$ACCOUNT" \
  --query "Budgets[?BudgetName=='ticketbottle-monthly'].BudgetLimit.Amount" --output text   # 40
aws ce get-anomaly-subscriptions --query "AnomalySubscriptions[].Subscribers[].Status" --output text  # CONFIRMED
```
Expected: `40`; anomaly subscriber status `CONFIRMED` (confirm the email if still `PENDING`).

- [ ] **Step 2: Substrate exists**
```bash
cd deploy/terraform/envs/foundation
terraform output vpc_id
terraform output dynamodb_table_arn
terraform output ci_role_arn
aws ecr describe-repositories --query "length(repositories)" --output text   # 13
```
Expected: non-empty VPC id, DynamoDB ARN, CI role ARN; 13 ECR repos.

- [ ] **Step 3: Images pushable**
Confirm Task 6 Step 5 shows `latest` in all 13 repos.

- [ ] **Step 4: Cost check**
```bash
aws ce get-cost-and-usage --time-period Start=$(date -u +%Y-%m-01),End=$(date -u +%Y-%m-%d) \
  --granularity MONTHLY --metrics UnblendedCost \
  --query "ResultsByTime[0].Total.UnblendedCost.Amount" --output text
```
Expected: a near-$0 figure (VPC/ECR/DynamoDB/IAM/Budget are all ~free at rest; only trivial ECR storage). If it is not near-zero, investigate before Phase A.

- [ ] **Step 5: Record outputs for Phase A**
Save `terraform output` values (VPC id, subnet ids, DynamoDB name/ARN, ECR URLs) into the Phase A working notes — Phase A's `envs/k3s` looks them up by tag but the outputs speed debugging.

- [ ] **Step 6: Final commit / branch state**
Ensure all Terraform + workflow changes are committed on `docs/aws-mac-offload-plan` (or merged to `main` so CI runs on merges going forward).

**Gate 0 (foundation) is green when:** budget + anomaly alarms are active and confirmed; VPC (2 public subnets, no NAT), 13 ECR repos, and the `ticketbottle-orders` DynamoDB table exist; CI has pushed `latest` to every repo via OIDC; month-to-date real cost is ~$0. **Only then does Phase A (k3s on EC2) begin.**

---

## Self-Review

**Spec coverage (Appendix B.7 Phase 0 + A.5/A.6):**
- Real-account prerequisites (root MFA, IAM admin, billing/Cost-Explorer, region lock) → Task 0. ✅
- Budget module first, on real/unblended spend → Task 1 (applied before all other resources; anomaly detection on unblended cost). ✅
- CI pipeline (GitHub Actions → ECR) → Task 6. ✅
- `vpc` / `ecr` / `dynamodb` / `iam` → Tasks 2/3/4/5. ✅
- Public-IPv4 trap (A.4), instance sizing (A.3), EC2 instance-profile IAM, `values-k3s.yaml`, health probes → **deferred to Phase A by design** (this plan stops at the substrate). Noted, not a gap.
- No NAT (Global Constraints + Task 2); PAY_PER_REQUEST (Task 4); no long-lived keys (Task 5/6). ✅

**Placeholder scan:** every code step contains complete HCL/YAML; no TBD/TODO; `target: ""` in the CI matrix is intentional (empty build target = full runtime image) and documented. ✅

**Type/name consistency:** `ci_role_arn` (module output → root output → `vars.AWS_CI_ROLE_ARN`); `repository_urls`/`image_names` list matches the CI matrix `repo` fields and `build-images.sh`/`build-migrate-images.sh` exactly; DynamoDB attributes/GSIs match `init-dynamodb.sh`; `local.tags` keys (`Project`/`ManagedBy`/`Env`) are used consistently for later tag lookups. ✅

**Open item flagged for Phase A (not a Phase-0 gap):** the payment **webhook** workload topology off-LocalStack (a re-hosted webhook Deployment vs. the `payment-events` adapter vs. real Lambda) is unresolved; Phase 0 hedges by keeping `payment-events` + `outbox-relay` images in ECR so either choice is unblocked.
