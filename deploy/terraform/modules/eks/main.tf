data "aws_partition" "current" {}

locals {
  # IAM conditions need the issuer host, not the URL: oidc.eks.us-east-1.amazonaws.com/id/ABC
  oidc_host = replace(aws_eks_cluster.this.identity[0].oidc[0].issuer, "https://", "")

  managed_policy_prefix = "arn:${data.aws_partition.current.partition}:iam::aws:policy"
}

# ---------------------------------------------------------------- control plane
resource "aws_iam_role" "cluster" {
  name = "${var.cluster_name}-cluster"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "eks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
  tags = var.tags
}

resource "aws_iam_role_policy_attachment" "cluster" {
  role       = aws_iam_role.cluster.name
  policy_arn = "${local.managed_policy_prefix}/AmazonEKSClusterPolicy"
}

resource "aws_eks_cluster" "this" {
  name     = var.cluster_name
  role_arn = aws_iam_role.cluster.arn
  version  = var.kubernetes_version

  vpc_config {
    subnet_ids             = var.subnet_ids
    endpoint_public_access = true
    public_access_cidrs    = [var.my_ip_cidr]

    # REQUIRED here, not optional: the nodes live in public subnets, so with only a
    # CIDR-restricted PUBLIC endpoint their kubelets would be refused by the API
    # server. Private access gives them an in-VPC path and keeps the public door
    # locked to one /32.
    endpoint_private_access = true
  }

  access_config {
    authentication_mode = "API"
    # The IAM principal running `terraform apply` becomes cluster-admin, so
    # `aws eks update-kubeconfig` just works with no aws-auth ConfigMap surgery.
    bootstrap_cluster_creator_admin_permissions = true
  }

  # enabled_cluster_log_types intentionally unset: control-plane logging creates a
  # CloudWatch log group that OUTLIVES `terraform destroy` and bills quietly.

  tags       = var.tags
  depends_on = [aws_iam_role_policy_attachment.cluster]
}

# The OIDC provider is what turns "a pod's projected ServiceAccount token" into
# something STS will trade for real AWS credentials. No provider, no IRSA.
data "tls_certificate" "oidc" {
  url = aws_eks_cluster.this.identity[0].oidc[0].issuer
}

resource "aws_iam_openid_connect_provider" "oidc" {
  url             = aws_eks_cluster.this.identity[0].oidc[0].issuer
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.oidc.certificates[0].sha1_fingerprint]
  tags            = var.tags
}

# ------------------------------------------------------------------ node group
# NOTE: this role has NO DynamoDB permission, on purpose. On Rung 2 the node's
# instance profile granted DynamoDB to every pod on the box. Here only the
# order-service ServiceAccount gets it (Task 5) — and the purchase flow passing is
# the proof that IRSA, not the node, is what authenticated.
resource "aws_iam_role" "node" {
  name = "${var.cluster_name}-node"
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

resource "aws_iam_role_policy_attachment" "node" {
  for_each = toset([
    "${local.managed_policy_prefix}/AmazonEKSWorkerNodePolicy",
    "${local.managed_policy_prefix}/AmazonEKS_CNI_Policy",
    # kubelet pulls straight from ECR — no regcred + systemd timer like k3s needed.
    "${local.managed_policy_prefix}/AmazonEC2ContainerRegistryReadOnly",
  ])
  role       = aws_iam_role.node.name
  policy_arn = each.value
}

resource "aws_eks_node_group" "spot" {
  cluster_name    = aws_eks_cluster.this.name
  node_group_name = "spot"
  node_role_arn   = aws_iam_role.node.arn
  subnet_ids      = var.subnet_ids
  capacity_type   = "SPOT"
  instance_types  = var.node_instance_types
  disk_size       = var.node_disk_gb

  scaling_config {
    desired_size = var.node_desired_size
    min_size     = var.node_min_size
    max_size     = var.node_max_size
  }

  update_config {
    max_unavailable = 1
  }

  lifecycle {
    # Let an autoscaler/HPA experiment (Phase C) move the node count without
    # Terraform fighting it back on the next apply.
    ignore_changes = [scaling_config[0].desired_size]
  }

  tags       = var.tags
  depends_on = [aws_iam_role_policy_attachment.node]
}

# ------------------------------------------------------------------ EBS CSI addon
# vpc-cni, kube-proxy and coredns are pre-installed by EKS. The EBS CSI driver is
# NOT — and without it every PVC in the chart sits Pending forever, because the
# in-tree AWS EBS provisioner was removed from Kubernetes. This addon is the single
# most commonly missed EKS prerequisite for stateful workloads.
module "ebs_csi_irsa" {
  source            = "../irsa-role"
  role_name         = "${var.cluster_name}-ebs-csi"
  oidc_provider_arn = aws_iam_openid_connect_provider.oidc.arn
  oidc_provider_url = local.oidc_host
  namespace         = "kube-system"
  service_account   = "ebs-csi-controller-sa"
  policy_arns       = { ebs_csi = "${local.managed_policy_prefix}/service-role/AmazonEBSCSIDriverPolicy" }
  tags              = var.tags
}

resource "aws_eks_addon" "ebs_csi" {
  cluster_name                = aws_eks_cluster.this.name
  addon_name                  = "aws-ebs-csi-driver"
  service_account_role_arn    = module.ebs_csi_irsa.role_arn
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update = "OVERWRITE"

  # Its controller Deployment needs somewhere to run.
  depends_on = [aws_eks_node_group.spot]

  tags = var.tags
}