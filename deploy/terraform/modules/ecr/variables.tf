variable "image_names" {
  type        = list(string)
  description = "ECR repo names (without registry host), e.g. ticketbottle/user."
}

variable "tags" {
  type    = map(string)
  default = {}
}