output "cluster_name" {
  value = aws_eks_cluster.this.name
}

output "cluster_endpoint" {
  value = aws_eks_cluster.this.endpoint
}

output "cluster_oidc_provider_arn" {
  value = aws_iam_openid_connect_provider.oidc.arn
}

output "cluster_oidc_provider_url" {
  # host form (no https://) — what IAM condition keys expect
  value = local.oidc_host
}

output "node_role_arn" {
  value = aws_iam_role.node.arn
}

output "cluster_security_group_id" {
  value = aws_eks_cluster.this.vpc_config[0].cluster_security_group_id
}