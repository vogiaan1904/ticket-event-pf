output "repository_urls" {
  value = { for name, repo in aws_ecr_repository.this : name => repo.repository_url }
}

output "repository_names" {
  value = [for repo in aws_ecr_repository.this : repo.name]
}