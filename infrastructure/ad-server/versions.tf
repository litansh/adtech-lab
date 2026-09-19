terraform {
  required_version = ">= 1.5"
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 6.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.4" }
    random  = { source = "hashicorp/random", version = "~> 3.6" }
  }
  # Local state for now. A remote backend is a follow-up: the old state bucket
  # was destroyed in Gate 0 and nothing here is worth coupling to a new one yet.
}

provider "aws" {
  region              = var.region
  profile             = var.profile
  allowed_account_ids = [var.account_id]
  default_tags {
    tags = {
      Project   = "adtech-lab"
      Component = "ad-server"
      ManagedBy = "terraform"
    }
  }
}
