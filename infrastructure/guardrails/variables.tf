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
  type        = string
  description = "Where budget and anomaly alerts are delivered."
}

variable "monthly_ceiling_usd" {
  type        = number
  default     = 100
  description = "Project ceiling from CLAUDE.md. $100 is the ceiling, not the target."
}

variable "enable_budget_freeze" {
  type        = bool
  default     = true
  description = "Attach a deny-new-expensive-resources policy at the freeze threshold."
}

variable "freeze_threshold_percent" {
  type        = number
  default     = 80
  description = "Percent of the ceiling at which the freeze policy attaches."
}
