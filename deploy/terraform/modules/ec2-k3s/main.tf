data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

resource "aws_key_pair" "this" {
  key_name   = "ticketbottle-k3s"
  public_key = file(var.ssh_public_key_path)
  tags       = var.tags
}

resource "aws_security_group" "this" {
  name = "ticketbottle-k3s"
  # Stale since 0038, kept: a new description replaces this attached group.
  description = "k3s box: SSH from my IP only; all egress"
  vpc_id      = var.vpc_id
  # No inbound at all: the box is reached through SSM Session Manager (0038).
  ingress = []
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  tags = merge(var.tags, { Name = "ticketbottle-k3s" })
}

# Instance profile: DynamoDB on the orders table + its indexes, and ECR pull.
resource "aws_iam_role" "node" {
  name = "ticketbottle-k3s-node"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
  tags = var.tags
}

data "aws_iam_policy_document" "node" {
  statement {
    sid    = "Dynamo"
    effect = "Allow"
    actions = [
      "dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem",
      "dynamodb:DeleteItem", "dynamodb:Query", "dynamodb:Scan",
      "dynamodb:BatchGetItem", "dynamodb:BatchWriteItem",
      "dynamodb:ConditionCheckItem", "dynamodb:DescribeTable",
    ]
    resources = [var.dynamodb_table_arn, "${var.dynamodb_table_arn}/index/*"]
  }
  statement {
    sid       = "EcrAuth"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }
  statement {
    sid    = "EcrPull"
    effect = "Allow"
    actions = [
      "ecr:BatchCheckLayerAvailability",
      "ecr:GetDownloadUrlForLayer",
      "ecr:BatchGetImage",
    ]
    resources = ["arn:aws:ecr:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:repository/ticketbottle/*"]
  }
}

resource "aws_iam_role_policy" "node" {
  name   = "k3s-node"
  role   = aws_iam_role.node.id
  policy = data.aws_iam_policy_document.node.json
}

# Lets the SSM agent register, so Session Manager can open SSH to the box (0038).
resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.node.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "node" {
  name = "ticketbottle-k3s-node"
  role = aws_iam_role.node.name
}

resource "aws_instance" "k3s" {
  ami                         = data.aws_ssm_parameter.al2023.value
  instance_type               = var.instance_type
  subnet_id                   = var.subnet_id
  vpc_security_group_ids      = [aws_security_group.this.id]
  iam_instance_profile        = aws_iam_instance_profile.node.name
  key_name                    = aws_key_pair.this.key_name
  associate_public_ip_address = true
  user_data                   = templatefile("${path.module}/user-data.sh.tftpl", { region = data.aws_region.current.name })

  metadata_options {
    http_tokens                 = "required" # IMDSv2
    http_put_response_hop_limit = 2          # allow pods to reach IMDS if ever needed
  }

  credit_specification {
    cpu_credits = var.cpu_credits
  }

  root_block_device {
    volume_type = "gp3"
    volume_size = var.root_volume_gb
    tags        = merge(var.tags, { Name = "ticketbottle-k3s-root" })
  }

  tags = merge(var.tags, { Name = "ticketbottle-k3s" })

  # ForceNew attributes that drift with nobody changing them. Unignored, an
  # unrelated apply (an SSH-only IP change) replaces the box and its data:
  #   ami                         -> the "latest" SSM alias is republished
  #   associate_public_ip_address -> reads false while the box is stopped
  # Take a new AMI deliberately: `terraform apply -replace=module.ec2_k3s.aws_instance.k3s`.
  lifecycle {
    ignore_changes = [ami, associate_public_ip_address]
  }
}