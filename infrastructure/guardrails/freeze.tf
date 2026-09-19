# ---------------------------------------------------------------------------
# Budget freeze: at the freeze threshold, AWS attaches this deny policy to the
# IAM user. It blocks CREATION of new billable resources.
#
# What it is NOT: it does not delete anything, does not close the account, and
# does not stop resources that already exist. A runaway Lambda already deployed
# keeps running -- that is the Cost Fuse's job (Phase 1), not this one.
# This is a backstop against new resource sprawl while you are asleep.
# ---------------------------------------------------------------------------
resource "aws_iam_policy" "budget_freeze" {
  count       = var.enable_budget_freeze ? 1 : 0
  name        = "adtech-lab-budget-freeze"
  description = "Attached automatically by AWS Budgets at the freeze threshold. Denies creation of new billable resources. Detach manually after investigating."

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid    = "FreezeNewBillableResourceCreation"
      Effect = "Deny"
      Action = [
        "lambda:CreateFunction",
        "dynamodb:CreateTable",
        "firehose:CreateDeliveryStream",
        "cloudfront:CreateDistribution",
        "apigateway:POST",
        "ec2:RunInstances",
        "ec2:CreateVolume",
        "ec2:AllocateAddress",
        "ec2:CreateNatGateway",
        "elasticloadbalancing:CreateLoadBalancer",
        "rds:CreateDBInstance",
        "rds:CreateDBCluster",
        "elasticache:CreateCacheCluster",
        "eks:CreateCluster",
        "ecs:CreateService",
        "ecs:RunTask",
        "kafka:CreateCluster",
        "es:CreateDomain",
        "opensearch:CreateDomain",
        "sagemaker:CreateEndpoint",
        "bedrock:InvokeModel",
        "bedrock:InvokeModelWithResponseStream",
      ]
      Resource = "*"
    }]
  })
}

resource "aws_iam_role" "budgets_action" {
  count = var.enable_budget_freeze ? 1 : 0
  name  = "adtech-lab-budgets-action"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "budgets.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = {
        StringEquals = { "aws:SourceAccount" = var.account_id }
        ArnLike      = { "aws:SourceArn" = "arn:aws:budgets::${var.account_id}:budget/*" }
      }
    }]
  })
}

resource "aws_iam_role_policy" "budgets_action" {
  count = var.enable_budget_freeze ? 1 : 0
  name  = "attach-freeze-policy"
  role  = aws_iam_role.budgets_action[0].id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid      = "AttachAndDetachTheFreezePolicy"
      Effect   = "Allow"
      Action   = ["iam:AttachUserPolicy", "iam:DetachUserPolicy"]
      Resource = "arn:aws:iam::${var.account_id}:user/litan"
      Condition = {
        ArnEquals = { "iam:PolicyARN" = aws_iam_policy.budget_freeze[0].arn }
      }
    }]
  })
}

resource "aws_budgets_budget_action" "freeze" {
  count              = var.enable_budget_freeze ? 1 : 0
  budget_name        = aws_budgets_budget.monthly_actual.name
  action_type        = "APPLY_IAM_POLICY"
  approval_model     = "AUTOMATIC"
  execution_role_arn = aws_iam_role.budgets_action[0].arn
  notification_type  = "ACTUAL"

  action_threshold {
    action_threshold_type  = "PERCENTAGE"
    action_threshold_value = var.freeze_threshold_percent
  }

  definition {
    iam_action_definition {
      policy_arn = aws_iam_policy.budget_freeze[0].arn
      users      = ["litan"]
    }
  }

  subscriber {
    address           = var.alert_email
    subscription_type = "EMAIL"
  }
}
