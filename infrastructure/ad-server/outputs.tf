output "api_endpoint" {
  value       = aws_apigatewayv2_api.main.api_endpoint
  description = "Origin for the CloudFront dynamic behaviours. Feed to the edge stack."
}

output "ad_server_function" { value = aws_lambda_function.ad_server.function_name }
output "control_plane_table" { value = aws_dynamodb_table.control_plane.name }
output "serving_state_table" { value = aws_dynamodb_table.serving_state.name }
output "events_bucket" { value = aws_s3_bucket.events.bucket }
output "athena_workgroup" { value = aws_athena_workgroup.main.name }

output "cost_safety" {
  value = {
    lambda_reserved_concurrency = coalesce(var.lambda_reserved_concurrency, 0) == 0 ? "unreserved (Free plan account limit is 10)" : tostring(var.lambda_reserved_concurrency)
    route_throttle_rps          = var.route_throttle_rate
    ad_fuse_alarms              = length(aws_cloudwatch_metric_alarm.ad_fuse)
    analytics_fuse_alarms       = length(aws_cloudwatch_metric_alarm.analytics_fuse)
    warning_alarms              = length(aws_cloudwatch_metric_alarm.warn)
  }
}

output "what_this_does_not_do" {
  value = "Reserved concurrency bounds simultaneous executions, not spend. Route throttling bounds rate, not total. The Cost Fuse bounds duration. NONE of these is a contractual billing cap -- AWS provides none for usage-based services, and CloudFront flat-rate plans are unavailable on the Free plan. See docs/cost-model.md."
}
