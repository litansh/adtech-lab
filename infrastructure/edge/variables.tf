variable "profile" {
  type        = string
  description = "AWS CLI profile. No default on purpose: set it in terraform.tfvars."
}

variable "account_id" {
  type        = string
  description = "The only AWS account this stack may touch. Set it in terraform.tfvars."
}

variable "domain" {
  type        = string
  default     = "xoxoxo.live"
  description = "Registered at GoDaddy. Route 53 serves DNS once NS is delegated."
}

# The API Gateway endpoint from the ad-server stack. Passed as a variable rather
# than read via remote state: apps/publisher and the platform are separate
# entities and their stacks stay decoupled. See ADR 0001.
variable "api_endpoint" {
  type        = string
  description = "e.g. https://abc123.execute-api.us-east-1.amazonaws.com -- terraform output api_endpoint from infrastructure/ad-server"
}

# ---------------------------------------------------------------------------
# Sequencing.
#
# ACM DNS validation only completes once GoDaddy delegates NS to Route 53,
# because ACM has to resolve the validation record publicly. So:
#
#   1. apply with enable_custom_domain = false  -> CloudFront on *.cloudfront.net
#   2. delegate NS at GoDaddy using the nameservers output
#   3. apply with enable_custom_domain = true   -> cert validates, domain attaches
#
# This lets the whole slice be tested end to end before DNS propagates, instead
# of blocking on a registrar change.
# ---------------------------------------------------------------------------
variable "enable_custom_domain" {
  type    = bool
  default = false
}
