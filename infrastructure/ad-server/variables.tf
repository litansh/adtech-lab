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

variable "alert_email" {
  type = string
}

# --- Cost safety -----------------------------------------------------------

# Reserved concurrency is UNAVAILABLE on this account.
#
# The AWS Free plan caps total account concurrency at 10, and AWS requires at
# least 10 to remain unreserved -- so no function may reserve anything above 0.
#
# The silver lining: the account limit of 10 is itself a hard throughput cap,
# and a tighter one than the 20 we would have reserved. The binding constraint
# on cost remains the API Gateway route throttle at 20 rps.
#
# The cost: cost-fuse can no longer reserve capacity for itself, so a flood
# saturating all 10 could in principle starve the very function whose job is to
# stop it. Documented honestly in docs/cost-model.md rather than papered over.
# Set to null to leave concurrency unreserved; set a number if the account limit
# is ever raised.
variable "lambda_reserved_concurrency" {
  type    = number
  default = null
}

variable "route_throttle_rate" {
  type        = number
  default     = 20
  description = "Requests/second per route. The binding constraint on origin cost under abuse."
}

variable "route_throttle_burst" {
  type    = number
  default = 40
}

# Cost Fuse thresholds, route-aware. Derived from ~500 sessions/day with a 10x
# growth allowance; see docs/architecture-proposal.md for the arithmetic.
variable "fuse_ad_request_per_min" {
  type    = number
  default = 300
}
variable "fuse_events_per_min" {
  type    = number
  default = 300
}
variable "fuse_collect_per_min" {
  type    = number
  default = 600
}
variable "warn_ad_request_per_min" {
  type    = number
  default = 60
}
variable "warn_collect_per_min" {
  type    = number
  default = 100
}
variable "fuse_ad_request_per_day" {
  type    = number
  default = 40000
}
variable "fuse_events_per_day" {
  type    = number
  default = 50000
}
variable "fuse_collect_per_day" {
  type    = number
  default = 100000
}

variable "log_retention_days" {
  type    = number
  default = 7
}
