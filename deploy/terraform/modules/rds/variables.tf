variable "identifier" {
  type        = string
  description = "Instance identifier, also the security group's name, e.g. ticketbottle-eks-payment."
}

variable "instance_class" {
  type = string
}

variable "engine_version" {
  type        = string
  default     = "15"
  description = "Major version only; RDS picks the minor. Matches the chart's postgres:15 image."
}

variable "username" {
  type        = string
  description = "Master user; the chart's postgres.user."
}

variable "vpc_id" {
  type = string
}

variable "db_subnet_group_name" {
  type = string
}

variable "client_security_group_id" {
  type        = string
  description = "The only security group allowed to reach 5432: the EKS cluster SG, which the nodes carry."
}

variable "tags" {
  type    = map(string)
  default = {}
}
