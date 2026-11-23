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

output "lbc_role_arn" {
  value = module.lbc_irsa.role_arn
}

output "vpc_id" {
  value = data.terraform_remote_state.foundation.outputs.vpc_id
}

output "service_role_arns" {
  description = "IRSA role per ServiceAccount, for serviceAccount.<name>.roleArn."
  value = merge(
    { order-service = module.order_irsa.role_arn },
    { for sa, m in module.postgres_client_irsa : sa => m.role_arn },
  )
}
output "node_placement" {
  value = var.private_nodes ? "private subnets, NAT egress" : "public subnets, no NAT"
}

output "nat_public_ip" {
  value = try(module.nat_egress[0].public_ip, null)
}

output "postgres_hosts" {
  description = "postgres.hosts for values-eks.yaml."
  value       = { for k, m in module.rds : k => m.address }
}

output "postgres_password_arns" {
  description = "postgres.passwordArns for the chart: one Secrets Manager secret per instance."
  value       = { for k, m in module.rds : k => m.password_secret_arn }
}
