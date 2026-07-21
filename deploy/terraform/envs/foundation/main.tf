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