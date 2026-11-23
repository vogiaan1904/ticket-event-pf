output "address" {
  value = aws_db_instance.this.address
}

output "password_secret_arn" {
  value = aws_secretsmanager_secret.password.arn
}
