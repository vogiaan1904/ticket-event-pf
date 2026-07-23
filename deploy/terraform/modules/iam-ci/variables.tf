variable "github_repo" {
  type        = string
  description = "owner/name of the repo allowed to assume this role."
}

variable "tags" {
  type    = map(string)
  default = {}
}