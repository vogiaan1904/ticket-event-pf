# One ephemeral Postgres instance for the EKS stateful tier, and its password.
# See docs/design/eks-stateful-tier.md#terraform-changes.

# Alphanumeric: the chart puts it inside a DSN, where # ? % / : would need encoding.
resource "random_password" "master" {
  length  = 40
  special = false
}

resource "aws_secretsmanager_secret" "password" {
  name                    = "${var.identifier}-password"
  recovery_window_in_days = 0 # deleted at destroy, so the next session can reuse the name
  tags                    = var.tags
}

resource "aws_secretsmanager_secret_version" "password" {
  secret_id     = aws_secretsmanager_secret.password.id
  secret_string = random_password.master.result
}

resource "aws_security_group" "this" {
  name        = var.identifier
  description = "Postgres from the cluster nodes only"
  vpc_id      = var.vpc_id
  tags        = merge(var.tags, { Name = var.identifier })
}

resource "aws_vpc_security_group_ingress_rule" "postgres" {
  security_group_id            = aws_security_group.this.id
  referenced_security_group_id = var.client_security_group_id
  ip_protocol                  = "tcp"
  from_port                    = 5432
  to_port                      = 5432
}

resource "aws_db_instance" "this" {
  identifier     = var.identifier
  engine         = "postgres"
  engine_version = var.engine_version
  instance_class = var.instance_class
  multi_az       = true

  allocated_storage = 20
  storage_type      = "gp3"
  storage_encrypted = true

  username = var.username
  password = random_password.master.result

  db_subnet_group_name   = var.db_subnet_group_name
  vpc_security_group_ids = [aws_security_group.this.id]
  publicly_accessible    = false

  # Ephemeral: destroy must never wait on, or leave behind, a snapshot.
  backup_retention_period  = 1
  skip_final_snapshot      = true
  delete_automated_backups = true
  deletion_protection      = false
  apply_immediately        = true

  tags = var.tags
}
