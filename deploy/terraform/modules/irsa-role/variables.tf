variable "role_name" {
  type        = string
  description = "IAM role name. Must be unique in the account."
}

variable "oidc_provider_arn" {
  type        = string
  description = "ARN of the cluster's IAM OIDC provider."
}

variable "oidc_provider_url" {
  type        = string
  description = "Cluster OIDC issuer WITHOUT the https:// prefix, e.g. oidc.eks.us-east-1.amazonaws.com/id/ABC123."
}

variable "namespace" {
  type        = string
  description = "Kubernetes namespace of the ServiceAccount allowed to assume this role."
}

variable "service_account" {
  type        = string
  description = "ServiceAccount name allowed to assume this role."
}

variable "policy_json" {
  type        = string
  default     = ""
  description = "Inline policy document. Empty to attach only managed policies."
}

variable "policy_arns" {
  type        = list(string)
  default     = []
  description = "Managed policy ARNs to attach."
}

variable "tags" {
  type    = map(string)
  default = {}
}