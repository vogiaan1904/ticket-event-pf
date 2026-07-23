variable "table_name" {
  type    = string
  default = "ticketbottle-orders"
}

variable "tags" {
  type    = map(string)
  default = {}
}