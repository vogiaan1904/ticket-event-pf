output "cluster_name" {
  value = module.eks.cluster_name
}

output "kubeconfig_command" {
  value = "aws eks update-kubeconfig --region us-east-1 --name ${module.eks.cluster_name}"
}

output "ecr_registry" {
  value = data.terraform_remote_state.foundation.outputs.ecr_registry
}

output "alb_subnets" {
  # comma-joined for the alb.ingress.kubernetes.io/subnets annotation
  value = join(",", data.terraform_remote_state.foundation.outputs.public_subnet_ids)
}

output "my_ip_cidr" {
  value = var.my_ip_cidr
}

output "lbc_role_arn" {
  value = module.lbc_irsa.role_arn
}

output "vpc_id" {
  value = data.terraform_remote_state.foundation.outputs.vpc_id
}

output "order_role_arn" {
  value = module.order_irsa.role_arn
}