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
  name        = "ticketbottle-k3s"
  description = "k3s box: SSH from my IP only; all egress"
  vpc_id      = var.vpc_id
  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.my_ip_cidr]
  }
  # EC2 Instance Connect proxies the SSH session from AWS's own IP, not the
  # browser's -- a fallback path that works even when var.my_ip_cidr can't
  # reach the box. us-east-1 range: ip-ranges.json, service EC2_INSTANCE_CONNECT.
  ingress {
    description = "EC2 Instance Connect"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["18.206.107.24/29"]
  }
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

  root_block_device {
    volume_type = "gp3"
    volume_size = var.root_volume_gb
    tags        = merge(var.tags, { Name = "ticketbottle-k3s-root" })
  }

  tags = merge(var.tags, { Name = "ticketbottle-k3s" })

  # al2023.value is the "latest" SSM alias, not a pin -- AWS republishing it
  # forces a replace (ami is a ForceNew attribute) on the next unrelated
  # apply, e.g. an SSH-only IP change. Ignored here; take a new AMI only via
  # a deliberate `terraform taint`.
  lifecycle {
    ignore_changes = [ami]
  }
}