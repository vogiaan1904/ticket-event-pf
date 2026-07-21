variable "alert_email" {
  type        = string
  description = "Email address to receive budget + anomaly alerts."
}

variable "monthly_budget_usd" {
  type        = number
  description = "Hard monthly panic ceiling in USD. Notifications fire at 50% and 100%."
  default     = 40
}

variable "tags" {
  type    = map(string)
  default = {}
}