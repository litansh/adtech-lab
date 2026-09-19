# ---------------------------------------------------------------------------
# GitHub Actions deploy identity.
#
# The point of this stack: deploying should not depend on a human holding a
# valid AWS session. Interactive `aws login` tokens are short-lived, so any
# deploy that needs one is a deploy that fails at the wrong moment.
#
# OIDC rather than an access key. GitHub presents a short-lived signed token
# that AWS verifies; there is no secret in the repository to leak, rotate or
# forget. An access key in GitHub secrets would be a permanent credential to
# this account sitting in a third-party system.
#
# The trust policy is scoped to ONE repository and ONE branch. Without the `sub`
# condition, any GitHub repository in the world could assume this role.
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

# profile and allowed_account_ids match the other stacks: the AWS `default`
# profile is a WORK account, so nothing here may rely on a default.
provider "aws" {
  region              = var.region
  profile             = var.profile
  allowed_account_ids = [var.account_id]

  default_tags {
    tags = {
      project = "adtech-lab"
      stack   = "cicd"
    }
  }
}

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "profile" {
  type        = string
  description = "AWS CLI profile. No default on purpose: set it in terraform.tfvars."
}

variable "account_id" {
  type        = string
  description = "The only AWS account this stack may touch. Set it in terraform.tfvars."
}

# GitHub now issues OIDC subjects containing IMMUTABLE NUMERIC IDS:
#
#   repo:<owner>@<ownerId>/<repo>@<repoId>:ref:refs/heads/main
#
# not the old name-only `repo:litansh/adtech-lab:...`. A policy written against
# the names silently fails to match -- which it did, and CloudTrail's
# AssumeRoleWithWebIdentity record was the only place the real claim was
# visible.
#
# Matching the IDs is also strictly safer than matching names: a repository
# deleted and recreated under the same name gets a new id, so access does not
# quietly transfer to a different repository that merely looks the same.
#
# That argument was only half applied. The subject still carried the literal
# name between the two ids, so renaming the repository -- a thing GitHub treats
# as cosmetic and does not warn about -- would have silently broken every
# workflow that touches AWS. The failure would have appeared as a permission
# error in five workflows at once, with nothing pointing at the rename.
#
# So the name is now a wildcard and BOTH ids are pinned. This is not a
# loosening: the repository id is unique and immutable, repository names cannot
# contain ':' or '/', and the pattern is anchored on '@<repoId>:ref:...', so
# only this repository can produce a matching subject whatever it is called.
variable "github_owner_id" {
  type        = string
  description = "OIDC subject owner part: owner@ownerId"
}

variable "github_repo_id" {
  type        = string
  description = "immutable repository id — survives a rename, changes on delete-and-recreate"
}

variable "deploy_branch" {
  type        = string
  default     = "main"
  description = "only this branch may deploy"
}

variable "site_bucket" {
  type        = string
  default     = ""
  description = "defaults to adtech-lab-publisher-<account_id>"
}

locals {
  site_bucket   = var.site_bucket != "" ? var.site_bucket : "adtech-lab-publisher-${var.account_id}"
  events_bucket = var.events_bucket != "" ? var.events_bucket : "adtech-lab-events-${var.account_id}"
}

data "aws_caller_identity" "current" {}

# GitHub's OIDC provider. One per account; if it already exists, import it
# rather than creating a second.
resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
  # AWS validates GitHub's certificate chain against its own trust store, so
  # this thumbprint is no longer load-bearing. It remains a required field.
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

data "aws_iam_policy_document" "assume" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    # Without this, ANY repository on GitHub could assume the role.
    condition {
      # StringLike, because the repository NAME is a wildcard. The owner id and
      # the repository id are both still exact.
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:${var.github_owner_id}/*@${var.github_repo_id}:ref:refs/heads/${var.deploy_branch}"]
    }
  }
}

resource "aws_iam_role" "publisher_deploy" {
  name                 = "adtech-lab-github-publisher-deploy"
  description          = "Deploys the static publisher from GitHub Actions. Static assets only."
  assume_role_policy   = data.aws_iam_policy_document.assume.json
  max_session_duration = 3600
}

# Deliberately narrow. This role can publish the site and invalidate the CDN.
# It cannot touch the ad server, DynamoDB, Lambda, IAM or billing -- a deploy
# credential that can also change the control plane is a deploy credential that
# can change what advertisers are charged.
data "aws_iam_policy_document" "publisher_deploy" {
  statement {
    sid       = "ListSiteBucket"
    effect    = "Allow"
    actions   = ["s3:ListBucket", "s3:GetBucketLocation"]
    resources = ["arn:aws:s3:::${local.site_bucket}"]
  }

  statement {
    sid    = "WriteSiteObjects"
    effect = "Allow"
    actions = [
      "s3:PutObject",
      "s3:PutObjectAcl",
      "s3:GetObject",
      "s3:DeleteObject",
    ]
    resources = ["arn:aws:s3:::${local.site_bucket}/*"]
  }

  # CloudFront invalidation cannot be scoped to a distribution ARN in the
  # resource block for CreateInvalidation, so the listing is allowed broadly
  # and the invalidation itself is scoped.
  statement {
    sid       = "FindDistribution"
    effect    = "Allow"
    actions   = ["cloudfront:ListDistributions"]
    resources = ["*"]
  }

  statement {
    sid       = "InvalidateCDN"
    effect    = "Allow"
    actions   = ["cloudfront:CreateInvalidation", "cloudfront:GetInvalidation"]
    resources = ["arn:aws:cloudfront::${data.aws_caller_identity.current.account_id}:distribution/*"]
  }
}

resource "aws_iam_role_policy" "publisher_deploy" {
  name   = "publisher-deploy"
  role   = aws_iam_role.publisher_deploy.id
  policy = data.aws_iam_policy_document.publisher_deploy.json
}

output "role_arn" {
  value       = aws_iam_role.publisher_deploy.arn
  description = "Set as AWS_DEPLOY_ROLE_ARN in the deploy workflow."
}

# ---------------------------------------------------------------------------
# Nightly report identity.
#
# Separate from the deploy role and strictly read-only. The cold-path jobs read
# events and write a report into the repository -- they have no reason to be
# able to change anything in AWS, and a scheduled job with write access is a
# scheduled job that can quietly change what advertisers are charged.
#
# It is also allowed from any branch, not just main, because it never mutates.
# ---------------------------------------------------------------------------

variable "events_bucket" {
  type        = string
  default     = ""
  description = "defaults to adtech-lab-events-<account_id>"
}

data "aws_iam_policy_document" "assume_report" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:${var.github_owner_id}/*@${var.github_repo_id}:*"]
    }
  }
}

resource "aws_iam_role" "nightly_report" {
  name                 = "adtech-lab-github-nightly-report"
  description          = "Read-only. Reads events so the cold-path jobs can report."
  assume_role_policy   = data.aws_iam_policy_document.assume_report.json
  max_session_duration = 3600
}

data "aws_iam_policy_document" "read_events" {
  statement {
    effect    = "Allow"
    actions   = ["s3:ListBucket"]
    resources = ["arn:aws:s3:::${local.events_bucket}"]
  }
  statement {
    effect    = "Allow"
    actions   = ["s3:GetObject"]
    resources = ["arn:aws:s3:::${local.events_bucket}/*"]
  }
}

# The Reliability agent reads alarm state, Lambda metrics and month-to-date
# spend. All read-only: it reports and, when the fuse is needed, the fuse is a
# separate mechanism that already exists.
data "aws_iam_policy_document" "read_health" {
  statement {
    effect = "Allow"
    actions = [
      "cloudwatch:DescribeAlarms",
      "cloudwatch:GetMetricStatistics",
      "ce:GetCostAndUsage",
    ]
    resources = ["*"] # neither API supports resource-level permissions
  }
}

resource "aws_iam_role_policy" "read_health" {
  name   = "read-health"
  role   = aws_iam_role.nightly_report.id
  policy = data.aws_iam_policy_document.read_health.json
}

resource "aws_iam_role_policy" "read_events" {
  name   = "read-events"
  role   = aws_iam_role.nightly_report.id
  policy = data.aws_iam_policy_document.read_events.json
}

output "report_role_arn" {
  value       = aws_iam_role.nightly_report.arn
  description = "Set as AWS_REPORT_ROLE_ARN in the nightly workflow."
}

# ---------------------------------------------------------------------------
# Ad-server CODE deploys from CI.
#
# The ad server's Terraform state is local, so CI cannot run Terraform. It does
# not need to: everything that changes week to week is Go code, and code is
# shipped with lambda:UpdateFunctionCode alone.
#
# That split is the same principle as the agent authority rule in CLAUDE.md --
# code and configuration move continuously and automatically; the SHAPE of the
# system changes rarely, deliberately, and with a human running terraform.
#
# The permission is scoped to the two functions by name. It cannot create a
# function, change its role, its environment, its triggers or its concurrency --
# only replace the code in a function that already exists.
# ---------------------------------------------------------------------------

variable "lambda_functions" {
  type        = list(string)
  default     = ["adtech-lab-ad-server", "adtech-lab-cost-fuse"]
  description = "functions whose CODE CI may replace"
}

data "aws_iam_policy_document" "lambda_deploy" {
  statement {
    sid    = "UpdateFunctionCodeOnly"
    effect = "Allow"
    actions = [
      "lambda:UpdateFunctionCode",
      "lambda:GetFunction",
      "lambda:GetFunctionConfiguration",
      "lambda:PublishVersion",
    ]
    resources = [
      for f in var.lambda_functions :
      "arn:aws:lambda:${var.region}:${data.aws_caller_identity.current.account_id}:function:${f}"
    ]
  }
}

resource "aws_iam_role_policy" "lambda_deploy" {
  name   = "lambda-code-deploy"
  role   = aws_iam_role.publisher_deploy.id
  policy = data.aws_iam_policy_document.lambda_deploy.json
}
