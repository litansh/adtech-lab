# ---------------------------------------------------------------------------
# Alerting
# ---------------------------------------------------------------------------
resource "aws_sns_topic" "alerts" {
  name = "adtech-lab-cost-alerts"
}

# Cost Anomaly Detection publishes here; SNS fans out to email.
data "aws_iam_policy_document" "alerts_topic" {
  statement {
    sid     = "AllowCostAnomalyDetectionToPublish"
    effect  = "Allow"
    actions = ["SNS:Publish"]
    principals {
      type        = "Service"
      identifiers = ["costalerts.amazonaws.com"]
    }
    resources = [aws_sns_topic.alerts.arn]
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [var.account_id]
    }
  }
}

resource "aws_sns_topic_policy" "alerts" {
  arn    = aws_sns_topic.alerts.arn
  policy = data.aws_iam_policy_document.alerts_topic.json
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# ---------------------------------------------------------------------------
# Budget: actual spend, notified at 20/40/60/80/100 % of the ceiling
# ---------------------------------------------------------------------------
resource "aws_budgets_budget" "monthly_actual" {
  name         = "adtech-lab-monthly-actual"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_ceiling_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  dynamic "notification" {
    for_each = [20, 40, 60, 80, 100]
    content {
      comparison_operator        = "GREATER_THAN"
      threshold                  = notification.value
      threshold_type             = "PERCENTAGE"
      notification_type          = "ACTUAL"
      subscriber_email_addresses = [var.alert_email]
    }
  }
}

# Forecast budget: warns before the money is actually spent.
resource "aws_budgets_budget" "monthly_forecast" {
  name         = "adtech-lab-monthly-forecast"
  budget_type  = "COST"
  limit_amount = tostring(var.monthly_ceiling_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alert_email]
  }
}

# ---------------------------------------------------------------------------
# Cost Anomaly Detection
# ---------------------------------------------------------------------------
resource "aws_ce_anomaly_monitor" "services" {
  name              = "adtech-lab-services"
  monitor_type      = "DIMENSIONAL"
  monitor_dimension = "SERVICE"
}

resource "aws_ce_anomaly_subscription" "alerts" {
  name      = "adtech-lab-anomalies"
  frequency = "IMMEDIATE"

  monitor_arn_list = [aws_ce_anomaly_monitor.services.arn]

  subscriber {
    type    = "SNS"
    address = aws_sns_topic.alerts.arn
  }

  depends_on = [aws_sns_topic_policy.alerts]

  # Alert on anomalies of $5 or more. At a $1-7/month baseline, $5 is a real signal.
  threshold_expression {
    dimension {
      key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
      values        = ["5"]
      match_options = ["GREATER_THAN_OR_EQUAL"]
    }
  }
}
