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