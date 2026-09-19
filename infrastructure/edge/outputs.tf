output "cloudfront_domain" {
  value       = aws_cloudfront_distribution.main.domain_name
  description = "Test here before DNS delegation."
}

output "site_bucket" { value = aws_s3_bucket.site.bucket }

output "nameservers" {
  value       = aws_route53_zone.main.name_servers
  description = "Set these four as the nameservers for xoxoxo.live at GoDaddy, then re-apply with enable_custom_domain=true."
}

output "next_step" {
  value = var.enable_custom_domain ? "Custom domain active. Verify https://${var.domain} serves the game room." : "1) deploy content: make deploy-publisher  2) test the CloudFront domain  3) delegate NS at GoDaddy  4) re-apply with -var enable_custom_domain=true"
}
