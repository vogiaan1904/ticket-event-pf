# A single monthly cost budget set to the PANIC ceiling ($40), with notifications
# at 50% ($20 = the real target) and 100% ($40), plus a forecast alarm. One budget
# keeps us inside the AWS Budgets free tier (first 2 budgets free).
resource "aws_budgets_budget" "monthly" {
  name         = "ticketbottle-monthly"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_budget_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  # Actual spend crosses the $20 target.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 50
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }

  # Actual spend hits the $40 panic ceiling.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }

  # Forecast says we will blow the ceiling this month.
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alert_email]
  }
}

# Cost Anomaly Detection: catches a forgotten resource that spikes cost between
# budget cycles. Monitors per-AWS-service spend, emails on any anomaly >= $5.
resource "aws_ce_anomaly_monitor" "service" {
  name              = "ticketbottle-service-monitor"
  monitor_type      = "DIMENSIONAL"
  monitor_dimension = "SERVICE"
  tags              = var.tags
}

resource "aws_ce_anomaly_subscription" "alerts" {
  name      = "ticketbottle-anomaly-alerts"
  frequency = "DAILY"

  monitor_arn_list = [aws_ce_anomaly_monitor.service.arn]

  subscriber {
    type    = "EMAIL"
    address = var.alert_email
  }

  threshold_expression {
    dimension {
      key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
      match_options = ["GREATER_THAN_OR_EQUAL"]
      values        = ["5"]
    }
  }

  tags = var.tags
}