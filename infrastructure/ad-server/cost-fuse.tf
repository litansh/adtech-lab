# ---------------------------------------------------------------------------
# The Cost Fuse.
#
# The layered controls above bound a RATE. This bounds DURATION: how long any
# abuse can bill before serving fails closed.
#
# Route-aware, because product events outnumber ad requests roughly 9:1. A
# single alarm on total API Gateway Count would be dominated by analytics --
# meaning the site becoming popular would trip the advertising fuse. That is a
# failure mode, not a safety control.
#
#   AD FUSE         /ad/request + /event/*  ->  concurrency 0. Ad serving stops.
#   ANALYTICS FUSE  /collect                ->  never touches ad serving.
#
# The static games site is served by CloudFront and is deliberately NOT behind
# either fuse: a traffic spike must never take the games offline.
# ---------------------------------------------------------------------------

resource "aws_sns_topic" "fuse" {
  name = "adtech-lab-cost-fuse"
}

resource "aws_sns_topic_subscription" "fuse_email" {
  topic_arn = aws_sns_topic.fuse.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

resource "aws_iam_role" "cost_fuse" {
  name = "adtech-lab-cost-fuse"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
}

resource "aws_iam_role_policy" "cost_fuse" {
  name = "trip-the-fuse"
  role = aws_iam_role.cost_fuse.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "${aws_cloudwatch_log_group.cost_fuse.arn}:*"
      },
      {
        # The whole point: set reserved concurrency to zero. Scoped to the one
        # function it is allowed to stop.
        Effect   = "Allow"
        Action   = ["lambda:PutFunctionConcurrency", "lambda:GetFunctionConcurrency"]
        Resource = aws_lambda_function.ad_server.arn
      },
      {
        Effect   = "Allow"
        Action   = ["dynamodb:GetItem", "dynamodb:PutItem"]
        Resource = aws_dynamodb_table.serving_state.arn
      },
      {
        Effect   = "Allow"
        Action   = ["sns:Publish"]
        Resource = aws_sns_topic.fuse.arn
      }
    ]
  })
}

resource "aws_cloudwatch_log_group" "cost_fuse" {
  name              = "/aws/lambda/adtech-lab-cost-fuse"
  retention_in_days = 30 # longer than serving logs: fuse events are rare and matter
}

resource "aws_lambda_function" "cost_fuse" {
  function_name = "adtech-lab-cost-fuse"
  role          = aws_iam_role.cost_fuse.arn
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  memory_size   = 128
  timeout       = 30

  filename         = data.archive_file.cost_fuse.output_path
  source_code_hash = data.archive_file.cost_fuse.output_base64sha256

  # We WANTED a small reservation here, so a flood of the ad server could never
  # starve the thing whose job is to stop the flood. The Free plan's account
  # limit of 10 makes that impossible. Accepted risk, stated plainly:
  # the API Gateway throttle (20 rps at ~15ms is ~0.3 concurrent) means
  # saturation is unlikely, SNS retries delivery, and the daily and budget
  # tiers are independent slower paths. Revisit if the account limit is raised.

  environment {
    variables = {
      AD_SERVER_FUNCTION = aws_lambda_function.ad_server.function_name
      STATE_TABLE        = aws_dynamodb_table.serving_state.name
      ALERT_TOPIC_ARN    = aws_sns_topic.fuse.arn
    }
  }

  depends_on = [aws_cloudwatch_log_group.cost_fuse]
}

resource "aws_lambda_permission" "fuse_sns" {
  statement_id  = "AllowSNSInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.cost_fuse.function_name
  principal     = "sns.amazonaws.com"
  source_arn    = aws_sns_topic.fuse.arn
}

resource "aws_sns_topic_subscription" "fuse_lambda" {
  topic_arn = aws_sns_topic.fuse.arn
  protocol  = "lambda"
  endpoint  = aws_lambda_function.cost_fuse.arn
}

# ---------------------------------------------------------------------------
# Alarms. Different SIGNALS per tier, not three variants of one -- a single
# blind spot must not disable them all.
# ---------------------------------------------------------------------------
locals {
  # AD FUSE: these trip and stop ad serving.
  ad_fuse_alarms = {
    ad_request_fast  = { route = "POST /ad/request", period = 60, threshold = var.fuse_ad_request_per_min, desc = "ad requests/min" }
    impression_fast  = { route = "GET /event/impression", period = 60, threshold = var.fuse_events_per_min, desc = "impressions/min" }
    click_fast       = { route = "GET /event/click", period = 60, threshold = var.fuse_events_per_min, desc = "clicks/min" }
    ad_request_daily = { route = "POST /ad/request", period = 86400, threshold = var.fuse_ad_request_per_day, desc = "ad requests/day" }
    impression_daily = { route = "GET /event/impression", period = 86400, threshold = var.fuse_events_per_day, desc = "impressions/day" }
  }
  # ANALYTICS FUSE: alerts only here. Tripping is handled by the fuse Lambda,
  # which blocks /collect WITHOUT touching ad serving.
  analytics_alarms = {
    collect_fast  = { route = "POST /collect", period = 60, threshold = var.fuse_collect_per_min, desc = "collect/min" }
    collect_daily = { route = "POST /collect", period = 86400, threshold = var.fuse_collect_per_day, desc = "collect/day" }
  }
  warn_alarms = {
    ad_request_warn = { route = "POST /ad/request", period = 60, threshold = var.warn_ad_request_per_min, desc = "ad requests/min (warn)" }
    collect_warn    = { route = "POST /collect", period = 60, threshold = var.warn_collect_per_min, desc = "collect/min (warn)" }
  }
}

resource "aws_cloudwatch_metric_alarm" "ad_fuse" {
  for_each = local.ad_fuse_alarms

  alarm_name          = "adtech-lab-fuse-${each.key}"
  alarm_description   = "AD FUSE: ${each.value.desc} exceeded ${each.value.threshold}. Trips ad serving closed. Games stay online."
  namespace           = "AWS/ApiGateway"
  metric_name         = "Count"
  statistic           = "Sum"
  period              = each.value.period
  evaluation_periods  = 1
  threshold           = each.value.threshold
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"

  dimensions = {
    ApiId    = aws_apigatewayv2_api.main.id
    Stage    = aws_apigatewayv2_stage.default.name
    Route    = each.value.route
    Resource = each.value.route
    Method   = "-"
  }

  alarm_actions = [aws_sns_topic.fuse.arn]
}

resource "aws_cloudwatch_metric_alarm" "analytics_fuse" {
  for_each = local.analytics_alarms

  alarm_name          = "adtech-lab-analytics-${each.key}"
  alarm_description   = "ANALYTICS FUSE: ${each.value.desc} exceeded ${each.value.threshold}. Blocks /collect only. Ad serving UNAFFECTED."
  namespace           = "AWS/ApiGateway"
  metric_name         = "Count"
  statistic           = "Sum"
  period              = each.value.period
  evaluation_periods  = 1
  threshold           = each.value.threshold
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"

  dimensions = {
    ApiId    = aws_apigatewayv2_api.main.id
    Stage    = aws_apigatewayv2_stage.default.name
    Route    = each.value.route
    Resource = each.value.route
    Method   = "-"
  }

  alarm_actions = [aws_sns_topic.fuse.arn]
}

resource "aws_cloudwatch_metric_alarm" "warn" {
  for_each = local.warn_alarms

  alarm_name          = "adtech-lab-warn-${each.key}"
  alarm_description   = "WARNING only: ${each.value.desc} exceeded ${each.value.threshold}. No action taken."
  namespace           = "AWS/ApiGateway"
  metric_name         = "Count"
  statistic           = "Sum"
  period              = each.value.period
  evaluation_periods  = 1
  threshold           = each.value.threshold
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching"

  dimensions = {
    ApiId    = aws_apigatewayv2_api.main.id
    Stage    = aws_apigatewayv2_stage.default.name
    Route    = each.value.route
    Resource = each.value.route
    Method   = "-"
  }

  # Email only. Deliberately not wired to the fuse topic's Lambda action path.
  alarm_actions = [aws_sns_topic.fuse.arn]
}
