terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws    = { source = "hashicorp/aws", version = "~> 5.0" }
    tls    = { source = "hashicorp/tls", version = "~> 4.0" }
    http   = { source = "hashicorp/http", version = "~> 3.4" }
    random = { source = "hashicorp/random", version = "~> 3.6" }
  }
  backend "s3" {}
}

provider "aws" {
  region = "us-east-1"
  default_tags {
    tags = local.tags
  }
}

locals {
  tags = {
    Project   = "ticketbottle"
    ManagedBy = "terraform"
    Env       = "eks"
  }
}

# Same pattern as envs/k3s: the account-wide foundation is read, never re-created.
data "terraform_remote_state" "foundation" {
  backend = "s3"
  config = {
    bucket = var.state_bucket
    key    = "foundation/terraform.tfstate"
    region = "us-east-1"
  }
}

locals {
  public_subnet_ids  = data.terraform_remote_state.foundation.outputs.public_subnet_ids
  private_subnet_ids = data.terraform_remote_state.foundation.outputs.private_subnet_ids

  node_subnet_ids = var.private_nodes ? local.private_subnet_ids : local.public_subnet_ids

  # x-ENI placement follows the nodes.
  cluster_subnet_ids = var.private_nodes ? concat(local.public_subnet_ids, local.private_subnet_ids) : local.public_subnet_ids
}

module "nat_egress" {
  count                  = var.private_nodes ? 1 : 0
  source                 = "../../modules/nat-egress"
  name                   = var.cluster_name
  public_subnet_id       = local.public_subnet_ids[0]
  private_route_table_id = data.terraform_remote_state.foundation.outputs.private_route_table_id
  tags                   = local.tags
}

module "eks" {
  source              = "../../modules/eks"
  cluster_name        = var.cluster_name
  kubernetes_version  = var.kubernetes_version
  subnet_ids          = local.cluster_subnet_ids
  node_subnet_ids     = local.node_subnet_ids
  my_ip_cidr          = var.my_ip_cidr
  node_instance_types = var.node_instance_types
  node_desired_size   = var.node_desired_size
  node_min_size       = var.node_min_size
  node_max_size       = var.node_max_size
  node_disk_gb        = var.node_disk_gb
  tags                = local.tags
}

# ------------------------------- AWS Load Balancer Controller identity ---------
# The controller's IAM policy is long and AWS revises it; fetch the upstream one
# rather than pasting a copy that silently goes stale.
data "http" "lbc_policy" {
  url = var.lbc_iam_policy_url
}

resource "aws_iam_policy" "lbc" {
  name        = "${var.cluster_name}-alb-controller"
  description = "Upstream AWS Load Balancer Controller policy"
  policy      = data.http.lbc_policy.response_body
  tags        = local.tags
}

module "lbc_irsa" {
  source            = "../../modules/irsa-role"
  role_name         = "${var.cluster_name}-alb-controller"
  oidc_provider_arn = module.eks.cluster_oidc_provider_arn
  oidc_provider_url = module.eks.cluster_oidc_provider_url
  namespace         = "kube-system"
  service_account   = "aws-load-balancer-controller"
  policy_arns       = { alb_controller = aws_iam_policy.lbc.arn }
  tags              = local.tags
}

# ------------------------------------------------ order-service identity (IRSA) -
data "aws_iam_policy_document" "order_dynamodb" {
  statement {
    sid    = "Dynamo"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
      "dynamodb:Query",
      "dynamodb:Scan",
      "dynamodb:BatchGetItem",
      "dynamodb:BatchWriteItem",
      "dynamodb:ConditionCheckItem",
      "dynamodb:DescribeTable",
    ]
    resources = [
      data.terraform_remote_state.foundation.outputs.dynamodb_table_arn,
      "${data.terraform_remote_state.foundation.outputs.dynamodb_table_arn}/index/*",
    ]
  }
}

module "order_irsa" {
  source            = "../../modules/irsa-role"
  role_name         = "${var.cluster_name}-order-service"
  oidc_provider_arn = module.eks.cluster_oidc_provider_arn
  oidc_provider_url = module.eks.cluster_oidc_provider_url
  namespace         = "ticketbottle"
  service_account   = "order-service"
  policy_json       = data.aws_iam_policy_document.order_dynamodb.json
  tags              = local.tags
}
# ------------------------------------------------ Postgres: RDS ×3, phase (b) ---
# Split and sizes: docs/design/eks-stateful-tier.md#why-these-three-instances, 0039, 0040.
locals {
  rds_instance_classes = {
    payment   = "db.t4g.micro"
    inventory = "db.t4g.small" # max_connections must exceed 4 replicas x 25 (0040)
    shared    = "db.t4g.small"
  }
  postgres_user = "root" # the chart's postgres.user
}

# Lives here, not in envs/foundation, so it is created and destroyed with the cluster.
resource "aws_db_subnet_group" "rds" {
  name       = "${var.cluster_name}-rds"
  subnet_ids = local.private_subnet_ids
  tags       = local.tags
}

module "rds" {
  for_each                 = local.rds_instance_classes
  source                   = "../../modules/rds"
  identifier               = "${var.cluster_name}-${each.key}"
  instance_class           = each.value
  username                 = local.postgres_user
  vpc_id                   = data.terraform_remote_state.foundation.outputs.vpc_id
  db_subnet_group_name     = aws_db_subnet_group.rds.name
  client_security_group_id = module.eks.cluster_security_group_id
  tags                     = local.tags
}

# ----------------------- Postgres credentials: CSI + per-service IRSA, phase (c) ---
# The driver mounts each host's password under the pod's own IRSA role; syncSecret
# copies it into a Secret, since the apps read env, not files.
resource "aws_eks_addon" "secrets_store_csi" {
  cluster_name                = module.eks.cluster_name
  addon_name                  = "aws-secrets-store-csi-driver-provider"
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "OVERWRITE"
  configuration_values = jsonencode({
    "secrets-store-csi-driver" = { syncSecret = { enabled = true } }
  })
  tags       = local.tags
  depends_on = [module.eks]
}

# ServiceAccount -> the one host whose password it may read.
locals {
  postgres_clients = {
    user-service      = "shared"
    event-service     = "shared"
    temporal          = "shared"
    payment-service   = "payment"
    inventory-service = "inventory"
  }
}

data "aws_iam_policy_document" "postgres_password" {
  for_each = local.postgres_clients
  statement {
    actions   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
    resources = [module.rds[each.value].password_secret_arn]
  }
}

module "postgres_client_irsa" {
  for_each          = local.postgres_clients
  source            = "../../modules/irsa-role"
  role_name         = "${var.cluster_name}-${each.key}"
  oidc_provider_arn = module.eks.cluster_oidc_provider_arn
  oidc_provider_url = module.eks.cluster_oidc_provider_url
  namespace         = "ticketbottle"
  service_account   = each.key
  policy_json       = data.aws_iam_policy_document.postgres_password[each.key].json
  tags              = local.tags
}
