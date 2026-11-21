# One ephemeral Postgres instance for the EKS stateful tier.
# See docs/design/eks-stateful-tier.md#terraform-changes.

resource "aws_security_group" "this" {
  name        = var.identifier
  description = "Postgres from the cluster's nodes only"
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
  # RDS generates the password and owns its Secrets Manager secret, which is
  # deleted with the instance; Terraform state never holds it.
  manage_master_user_password = true

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
