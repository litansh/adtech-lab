# ---------------------------------------------------------------------------
# Ad server: Go on ARM64 Lambda.
# ---------------------------------------------------------------------------
resource "aws_iam_role" "ad_server" {
  name = "adtech-lab-ad-server"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole" }]
  })
}

# Least privilege: named resources only, never "*".
resource "aws_iam_role_policy" "ad_server" {
  name = "serve-ads"
  role = aws_iam_role.ad_server.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "${aws_cloudwatch_log_group.ad_server.arn}:*"
      },
      {
        Effect   = "Allow"
        Action   = ["dynamodb:GetItem", "dynamodb:Query", "dynamodb:Scan"]
        Resource = aws_dynamodb_table.control_plane.arn
      },
      {
        # UpdateItem only: the serving counter is an atomic ADD. No delete.
        Effect   = "Allow"
        Action   = ["dynamodb:GetItem", "dynamodb:UpdateItem"]
        Resource = aws_dynamodb_table.serving_state.arn
      },
      {
        # Write-only, and only under the events/ prefix. The ad server can
        # append to the ledger; it can never read or delete it.
        Effect   = "Allow"
        Action   = ["s3:PutObject"]
        Resource = "${aws_s3_bucket.events.arn}/events/*"
      }
    ]
  })
}

resource "aws_cloudwatch_log_group" "ad_server" {
  name              = "/aws/lambda/adtech-lab-ad-server"
  retention_in_days = var.log_retention_days
}

resource "aws_lambda_function" "ad_server" {
  function_name = "adtech-lab-ad-server"
  role          = aws_iam_role.ad_server.arn
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  memory_size   = 128
  timeout       = 5

  filename         = data.archive_file.ad_server.output_path
  source_code_hash = data.archive_file.ad_server.output_base64sha256

  # Unreserved: the Free plan's account-wide limit of 10 forbids reserving.
  # That account limit is itself the throughput ceiling. See variables.tf.
  reserved_concurrent_executions = var.lambda_reserved_concurrency

  environment {
    variables = {
      ADLAB_ENV                 = "production" # Decision Trace is NEVER returned
      ADLAB_TOKEN_KEY           = random_password.token_key.result
      ADLAB_CONTROL_PLANE_TABLE = aws_dynamodb_table.control_plane.name
      ADLAB_SERVING_STATE_TABLE = aws_dynamodb_table.serving_state.name
      ADLAB_EVENTS_BUCKET       = aws_s3_bucket.events.bucket
      ADLAB_LOG_SAMPLE_RATE     = "0.01" # 1% of ordinary requests; errors always
    }
  }

  depends_on = [aws_cloudwatch_log_group.ad_server]
}

# ---------------------------------------------------------------------------
# API Gateway HTTP API.
#
# Chosen over a Lambda Function URL despite costing $1.00/M: Function URLs have
# no throttling, and with CloudFront OAC a browser POST must compute and send
# its own x-amz-content-sha256. Per-route throttling is a real cost control we
# need; the payload-hash dance buys nothing. See docs/architecture-proposal.md.
# ---------------------------------------------------------------------------
resource "aws_apigatewayv2_api" "main" {
  name          = "adtech-lab"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "ad_server" {
  api_id                 = aws_apigatewayv2_api.main.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.ad_server.invoke_arn
  payload_format_version = "2.0"
  timeout_milliseconds   = 5000
}

locals {
  routes = {
    ad_request = "POST /ad/request"
    impression = "GET /event/impression"
    click      = "GET /event/click"
    collect    = "POST /collect"
  }
}

resource "aws_apigatewayv2_route" "r" {
  for_each  = local.routes
  api_id    = aws_apigatewayv2_api.main.id
  route_key = each.value
  target    = "integrations/${aws_apigatewayv2_integration.ad_server.id}"
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/apigateway/adtech-lab"
  retention_in_days = var.log_retention_days
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.main.id
  name        = "$default"
  auto_deploy = true

  # Per-route throttling: the binding constraint on origin cost under abuse.
  dynamic "route_settings" {
    for_each = local.routes
    content {
      route_key                = route_settings.value
      throttling_rate_limit    = var.route_throttle_rate
      throttling_burst_limit   = var.route_throttle_burst
      detailed_metrics_enabled = true # per-route Count metric -> the Cost Fuse
    }
  }

  default_route_settings {
    throttling_rate_limit  = var.route_throttle_rate
    throttling_burst_limit = var.route_throttle_burst
  }

  # Route settings reference routes by key, so the routes must exist first.
  depends_on = [aws_apigatewayv2_route.r]

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api.arn
    format = jsonencode({
      requestId = "$context.requestId", routeKey = "$context.routeKey",
      status    = "$context.status", latency = "$context.responseLatency",
      ip        = "$context.identity.sourceIp"
    })
  }
}

resource "aws_lambda_permission" "api" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ad_server.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.main.execution_arn}/*/*"
}
