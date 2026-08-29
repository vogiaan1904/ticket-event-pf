variable "vpc_id" { type = string }
variable "subnet_id" { type = string }
variable "dynamodb_table_arn" { type = string }
variable "ssh_public_key_path" {
  type        = string
  description = "Path to your SSH public key, e.g. ~/.ssh/id_ed25519.pub"
}
variable "my_ip_cidr" {
  type        = string
  description = "Your public IP as a /32, allowed for SSH. e.g. 203.0.113.7/32"
}
variable "instance_type" {
  type    = string
  default = "t3.large"
}
variable "root_volume_gb" {
  type    = number
  default = 50
}
variable "tags" {
  type    = map(string)
  default = {}
}