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