terraform {
  required_version = ">= 1.10.0"
  required_providers {
    aws  = { source = "hashicorp/aws", version = "~> 5.0" }
    tls  = { source = "hashicorp/tls", version = "~> 4.0" }
    http = { source = "hashicorp/http", version = "~> 3.4" }
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

module "eks" {
  source              = "../../modules/eks"
  cluster_name        = var.cluster_name
  kubernetes_version  = var.kubernetes_version
  subnet_ids          = data.terraform_remote_state.foundation.outputs.public_subnet_ids
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
# rather than pasting a copy that silently goes stale. An ephemeral cluster tracking
# `main` is the right trade — production would pin the tag matching its chart
# version and review the diff.
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

# ------------------------------------------- order-service identity (the point) -
# Exactly one workload may touch the orders table, and it proves who it is with a
# ServiceAccount token — not with a key, and not by inheriting the node's role
# (which has no DynamoDB permission at all).
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