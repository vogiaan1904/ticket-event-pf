variable "state_bucket" {
  type        = string
  description = "S3 bucket holding foundation + eks state, e.g. ticketbottle-tfstate-<account>."
}

variable "my_ip_cidr" {
  type        = string
  description = "Your public IP as a /32. Gates the cluster API endpoint AND the ALB."
}

variable "cluster_name" {
  type    = string
  default = "ticketbottle-eks"
}

variable "kubernetes_version" {
  type        = string
  default     = null
  description = "null = current EKS default (standard support, $0.10/hr). Never pin an EOL version ($0.60/hr)."
}

variable "node_instance_types" {
  type    = list(string)
  default = ["t3.large", "t3a.large"]
}

variable "node_desired_size" {
  type    = number
  default = 2
}

variable "node_min_size" {
  type    = number
  default = 2
}

variable "node_max_size" {
  type    = number
  default = 3
}

variable "node_disk_gb" {
  type    = number
  default = 30
}

variable "lbc_iam_policy_url" {
  type        = string
  default     = "https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/main/docs/install/iam_policy.json"
  description = "Upstream IAM policy for the AWS Load Balancer Controller."
}