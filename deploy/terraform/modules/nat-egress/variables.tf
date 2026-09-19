variable "name" {
  type        = string
  description = "Prefix for the NAT gateway and its EIP, e.g. the cluster name."
}

variable "public_subnet_id" {
  type        = string
  description = "A NAT gateway must sit in a PUBLIC subnet to reach the internet gateway."
}

variable "private_route_table_id" {
  type        = string
  description = "Route table of the private subnets that gain egress."
}

variable "tags" {
  type    = map(string)
  default = {}
}
