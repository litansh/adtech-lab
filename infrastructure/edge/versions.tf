terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

# CloudFront's ACM certificate MUST live in us-east-1, regardless of where
# anything else sits. That is why this stack is pinned there.
provider "aws" {
  region              = "us-east-1"
  profile             = var.profile
  allowed_account_ids = [var.account_id]
  default_tags {
    tags = {
      Project   = "adtech-lab"
      Component = "edge"
      ManagedBy = "terraform"
    }
  }
}
