variable "cluster_name" {
  type = string
}

variable "kubernetes_version" {
  type        = string
  default     = null
  description = <<-EOT
    Leave null to take the current EKS default, which is ALWAYS in standard support
    ($0.10/hr). Pinning an out-of-support version silently moves the cluster to
    EXTENDED support at $0.60/hr — 6x the cost.
  EOT
}

variable "subnet_ids" {
  type        = list(string)
  description = "At least two subnets in different AZs (EKS requires it; the ALB needs it)."
}

variable "my_ip_cidr" {
  type        = string
  description = "Your public IP as a /32. The only CIDR allowed to reach the public API endpoint."
}

variable "node_instance_types" {
  type        = list(string)
  default     = ["t3.large", "t3a.large"]
  description = "Multiple types widen the spot capacity pool and cut interruption risk."
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

variable "tags" {
  type    = map(string)
  default = {}
}