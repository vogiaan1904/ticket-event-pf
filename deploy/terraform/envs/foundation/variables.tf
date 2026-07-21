variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "alert_email" {
  type        = string
  description = "Budget + anomaly alert email."
}

variable "github_repo" {
  type        = string
  description = "GitHub repo allowed to assume the CI role, as owner/name."
  default     = "vogiaan1904/ticket-event-pf"
}

variable "monthly_budget_usd" {
  type    = number
  default = 40
}