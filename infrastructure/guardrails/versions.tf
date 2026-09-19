terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
  # Local state deliberately: this stack must exist BEFORE any S3 backend,
  # and it is the thing that protects the account while everything else is built.
  # Migrated to a remote backend in Phase 1 once the state bucket exists.
}

provider "aws" {
  region              = var.region
  profile             = var.profile
  allowed_account_ids = [var.account_id] # hard stop if the wrong account is ever targeted
  default_tags { tags = { Project = "adtech-lab", Component = "guardrails", ManagedBy = "terraform" } }
}
