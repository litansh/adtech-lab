output "alert_email" { value = var.alert_email }
output "monthly_ceiling_usd" { value = var.monthly_ceiling_usd }
output "freeze_enabled" { value = var.enable_budget_freeze }
output "freeze_threshold_usd" {
  value = var.enable_budget_freeze ? var.monthly_ceiling_usd * var.freeze_threshold_percent / 100 : null
}
output "what_this_does_not_do" {
  value = "These controls ALERT and block NEW resource creation. They do not stop resources that already exist and they are not a hard billing cap. AWS provides no hard billing limit. Runaway serving is the Cost Fuse's job in Phase 1."
}
