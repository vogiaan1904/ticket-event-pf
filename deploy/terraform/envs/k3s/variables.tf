variable "state_bucket" { type = string }
variable "ssh_public_key_path" { type = string }
variable "my_ip_cidr" { type = string }
variable "instance_type" {
  type    = string
  default = "t3.large"
}