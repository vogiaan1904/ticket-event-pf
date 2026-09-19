variable "cidr_block" {
  type    = string
  default = "10.0.0.0/16"
}

variable "public_subnet_cidrs" {
  type    = list(string)
  default = ["10.0.1.0/24", "10.0.2.0/24"]
}

variable "tags" {
  type    = map(string)
  default = {}
}

variable "private_subnet_cidrs" {
  type        = list(string)
  default     = ["10.0.11.0/24", "10.0.12.0/24"]
  description = "Subnets with no 0.0.0.0/0 route. Free standing empty; egress is a NAT gateway owned by the consuming env."
}
