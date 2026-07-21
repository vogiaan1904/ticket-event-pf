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

module "vpc" {
  source = "../../modules/vpc"
  tags   = local.tags
}

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
