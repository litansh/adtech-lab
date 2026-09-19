# ---------------------------------------------------------------------------
# A scheduler that actually schedules.
#
# GitHub's cron could not deliver a briefing at a known time. Measured over two
# days on this repository: scheduled runs 5-6 hours late, then not at all --
# every workflow, all six, silent past every slot. Their documentation is honest
# about it ("may be delayed or not run"), and scheduled workflows on a private
# repository are queued at low priority.
#
# That is acceptable for a nightly report and unacceptable for a briefing whose
# whole value is arriving at a known time. It is also unfixable from inside
# GitHub: the previous attempt scheduled eight hourly slots so a late one would
# still land, and the failure mode was that none of them ran.
#
# So the clock moves out of GitHub and into EventBridge, which is a scheduler
# rather than a best effort -- and which understands named timezones, so the
# DST hack disappears with it.
#
# Cost: one invocation a day. EventBridge Scheduler and Lambda are both inside
# the free tier at this volume; the marginal cost is zero and the ceiling is
# untouched.
# ---------------------------------------------------------------------------

terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region              = var.region
  profile             = var.profile
  allowed_account_ids = [var.account_id]

  default_tags {
    tags = {
      project = "adtech-lab"
      stack   = "scheduler"
    }
  }
}

variable "region" { default = "us-east-1" }
# No defaults on purpose: set them in terraform.tfvars.
variable "profile" { type = string }
variable "account_id" { type = string }
variable "repo" {
  type        = string
  description = "owner/name of the GitHub repository whose workflows are dispatched"
}

variable "briefing_time" {
  type        = string
  default     = "cron(0 9 * * ? *)"
  description = "09:00, interpreted in the timezone below"
}

variable "timezone" {
  type        = string
  default     = "Asia/Jerusalem"
  description = "A named zone, so summer and winter are the same line"
}

# ---------------------------------------------------------------------------
# The token.
#
# Created by hand and never by Terraform: a fine-grained personal access token
# scoped to this ONE repository with `actions: write` and nothing else. It is
# put into the secret by a human running `aws secretsmanager put-secret-value`,
# so its value never enters a state file, a plan output or this repository.
#
# ignore_changes on the value for the same reason -- Terraform manages the
# container, a person manages the contents.
# ---------------------------------------------------------------------------
resource "aws_secretsmanager_secret" "github_token" {
  name                    = "adtech-lab/github-dispatch-token"
  description             = "Fine-grained PAT, this repo only, actions:write. Set by hand."
  recovery_window_in_days = 7
}

# A placeholder so the stack applies before the token exists. The real value is
# written by hand; Terraform must never overwrite it on a later apply.
resource "aws_secretsmanager_secret_version" "placeholder" {
  secret_id     = aws_secretsmanager_secret.github_token.id
  secret_string = "replace-me"

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# ---------------------------------------------------------------------------
# The function
# ---------------------------------------------------------------------------
data "archive_file" "lambda" {
  type        = "zip"
  source_dir  = "${path.module}/lambda"
  output_path = "${path.module}/build/scheduler.zip"
}

resource "aws_iam_role" "lambda" {
  name = "adtech-lab-scheduler"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

# Exactly one secret, read-only. Not secretsmanager:* -- a scheduler that can
# read every secret in the account is a scheduler worth stealing.
resource "aws_iam_role_policy" "lambda" {
  name = "read-the-one-token"
  role = aws_iam_role.lambda.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = aws_secretsmanager_secret.github_token.arn
      },
      {
        Effect   = "Allow"
        Action   = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "arn:aws:logs:${var.region}:${var.account_id}:*"
      }
    ]
  })
}

resource "aws_lambda_function" "dispatch" {
  function_name    = "adtech-lab-dispatch"
  role             = aws_iam_role.lambda.arn
  handler          = "handler.handler"
  runtime          = "python3.12"
  architectures    = ["arm64"]
  timeout          = 20
  memory_size      = 128
  filename         = data.archive_file.lambda.output_path
  source_code_hash = data.archive_file.lambda.output_base64sha256

  environment {
    variables = {
      REPO             = var.repo
      WORKFLOW         = "orchestrator.yml"
      REF              = "main"
      TOKEN_SECRET_ARN = aws_secretsmanager_secret.github_token.arn
    }
  }
}

# Fourteen days: long enough to explain a missed morning, short enough to cost
# nothing.
resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${aws_lambda_function.dispatch.function_name}"
  retention_in_days = 14
}

# ---------------------------------------------------------------------------
# The clock
# ---------------------------------------------------------------------------
resource "aws_iam_role" "scheduler" {
  name = "adtech-lab-scheduler-invoke"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Principal = { Service = "scheduler.amazonaws.com" }
      Condition = { StringEquals = { "aws:SourceAccount" = var.account_id } }
    }]
  })
}

resource "aws_iam_role_policy" "scheduler" {
  name = "invoke-the-one-function"
  role = aws_iam_role.scheduler.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = aws_lambda_function.dispatch.arn
    }]
  })
}

resource "aws_scheduler_schedule" "briefing" {
  name                         = "adtech-lab-morning-briefing"
  schedule_expression          = var.briefing_time
  schedule_expression_timezone = var.timezone

  flexible_time_window {
    mode = "OFF" # on time, which is the entire point
  }

  target {
    arn      = aws_lambda_function.dispatch.arn
    role_arn = aws_iam_role.scheduler.arn
    input    = jsonencode({ workflow = "orchestrator.yml" })

    retry_policy {
      maximum_retry_attempts       = 3
      maximum_event_age_in_seconds = 3600
    }
  }
}

# A dispatch that fails is a morning with no briefing, which is the failure this
# stack exists to remove. It must be visible rather than merely absent.
resource "aws_cloudwatch_metric_alarm" "dispatch_failed" {
  alarm_name          = "adtech-lab-dispatch-failed"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 3600
  statistic           = "Sum"
  threshold           = 1
  treat_missing_data  = "notBreaching"
  dimensions          = { FunctionName = aws_lambda_function.dispatch.function_name }
  alarm_description   = "The morning briefing was not dispatched."
}

output "secret_arn" { value = aws_secretsmanager_secret.github_token.arn }
output "function_name" { value = aws_lambda_function.dispatch.function_name }
output "next_step" {
  value = "Create a fine-grained PAT (this repo, actions:write), then: aws secretsmanager put-secret-value --secret-id ${aws_secretsmanager_secret.github_token.id} --secret-string 'TOKEN' --profile ${var.profile}"
}
