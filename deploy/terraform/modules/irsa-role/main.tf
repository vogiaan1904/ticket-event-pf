# IRSA = the same web-identity federation the CI role uses (modules/iam-ci), but the
# identity provider is the CLUSTER's OIDC issuer and the subject is a ServiceAccount
# instead of a GitHub repo. The `sub` condition is what makes it least-privilege:
# only pods running as system:serviceaccount:<ns>:<sa> can mint these credentials.
data "aws_iam_policy_document" "assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [var.oidc_provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${var.oidc_provider_url}:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "${var.oidc_provider_url}:sub"
      values   = ["system:serviceaccount:${var.namespace}:${var.service_account}"]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = var.role_name
  assume_role_policy = data.aws_iam_policy_document.assume.json
  tags               = var.tags
}

resource "aws_iam_role_policy" "inline" {
  count  = var.policy_json == "" ? 0 : 1
  name   = "inline"
  role   = aws_iam_role.this.id
  policy = var.policy_json
}

resource "aws_iam_role_policy_attachment" "managed" {
  for_each   = var.policy_arns          # was: toset(var.policy_arns)
  role       = aws_iam_role.this.name
  policy_arn = each.value
}