# ---------------------------------------------------------------------------
# The Publisher's static site. Private bucket; only CloudFront can read it.
# Terraform owns the bucket, NOT its contents -- content is a deploy step
# (`make deploy-publisher`), because terraform destroy must never be able to
# take the site's files with it.
# ---------------------------------------------------------------------------
resource "aws_s3_bucket" "site" {
  bucket = "adtech-lab-publisher-${var.account_id}"
}

resource "aws_s3_bucket_public_access_block" "site" {
  bucket                  = aws_s3_bucket.site.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_cloudfront_origin_access_control" "site" {
  name                              = "adtech-lab-site"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

data "aws_iam_policy_document" "site" {
  statement {
    # GetObject serves the site. ListBucket is granted deliberately: without it
    # S3 answers 403 for a MISSING key rather than 404, which is
    # indistinguishable from the ad server's legitimate 403 on a forged
    # tracking token. Distribution-wide custom error responses cannot tell the
    # two apart, so the fix is to make S3 return the right status in the first
    # place. It exposes nothing -- the bucket stays private and only CloudFront
    # can read it.
    actions = ["s3:GetObject", "s3:ListBucket"]
    resources = [
      aws_s3_bucket.site.arn,
      "${aws_s3_bucket.site.arn}/*",
    ]
    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.main.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "site" {
  bucket = aws_s3_bucket.site.id
  policy = data.aws_iam_policy_document.site.json
}

# ---------------------------------------------------------------------------
# CloudFront: one distribution, two origins, one domain.
#
# Pay-as-you-go. Flat-rate plans -- and their "no overage charges" guarantee --
# are unavailable on the AWS Free plan. Always-free tier still applies:
# 10M requests and 1TB egress per month.
#
# One domain matters: adtag.js posts to a RELATIVE path, so the Publisher and
# the ad server must appear as one origin to the browser. That is also exactly
# how a real publisher's first-party ad endpoint behaves.
# ---------------------------------------------------------------------------
locals {
  s3_origin  = "s3-site"
  api_origin = "api-adserver"
  api_host   = replace(var.api_endpoint, "https://", "")
}

# Static assets are content-hashed, so they can be cached hard and long.
resource "aws_cloudfront_cache_policy" "static" {
  name        = "adtech-lab-static"
  default_ttl = 86400
  max_ttl     = 31536000
  min_ttl     = 0
  parameters_in_cache_key_and_forwarded_to_origin {
    cookies_config { cookie_behavior = "none" }
    headers_config { header_behavior = "none" }
    query_strings_config { query_string_behavior = "none" }
    enable_accept_encoding_gzip   = true
    enable_accept_encoding_brotli = true
  }
}

# Ad decisions must never be cached: two users must not share an impression.
# CloudFront rejects query-string configuration on a policy with caching
# disabled, so use the AWS-managed CachingDisabled policy rather than a custom
# one that says the same thing.
data "aws_cloudfront_cache_policy" "disabled" {
  name = "Managed-CachingDisabled"
}

# Forward the viewer's country to the origin. This is how the ad server does
# geo targeting without ever seeing or storing a raw IP -- see
# docs/privacy-baseline.md.
resource "aws_cloudfront_origin_request_policy" "dynamic" {
  name = "adtech-lab-dynamic-origin"
  cookies_config { cookie_behavior = "none" }
  query_strings_config { query_string_behavior = "all" }
  headers_config {
    header_behavior = "whitelist"
    headers { items = ["CloudFront-Viewer-Country", "Content-Type", "User-Agent"] }
  }
}

resource "aws_cloudfront_function" "rewrite_index" {
  name    = "adtech-lab-rewrite-index"
  runtime = "cloudfront-js-2.0"
  comment = "Map directory-style paths onto index.html; S3 has no directories."
  publish = true
  code    = file("${path.module}/functions/rewrite-index.js")
}

resource "aws_cloudfront_distribution" "main" {
  enabled             = true
  default_root_object = "index.html"
  comment             = "adtech-lab publisher + ad server"
  price_class         = "PriceClass_100" # NA + EU only: cheapest, and where our traffic is

  aliases = var.enable_custom_domain ? [var.domain] : []

  origin {
    origin_id                = local.s3_origin
    domain_name              = aws_s3_bucket.site.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.site.id
  }

  origin {
    origin_id   = local.api_origin
    domain_name = local.api_host
    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id       = local.s3_origin
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    cache_policy_id        = aws_cloudfront_cache_policy.static.id
    compress               = true

    function_association {
      event_type   = "viewer-request"
      function_arn = aws_cloudfront_function.rewrite_index.arn
    }
  }

  # Dynamic behaviours. Deliberately NOT behind the Cost Fuse's static exclusion:
  # these are the routes the fuse protects.
  dynamic "ordered_cache_behavior" {
    for_each = ["/ad/*", "/event/*", "/collect"]
    content {
      path_pattern             = ordered_cache_behavior.value
      target_origin_id         = local.api_origin
      viewer_protocol_policy   = "https-only"
      allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
      cached_methods           = ["GET", "HEAD"]
      cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
      origin_request_policy_id = aws_cloudfront_origin_request_policy.dynamic.id
      compress                 = true
    }
  }

  # The games are a client-side SPA-ish set of static routes; a missing path
  # should show the site, not an XML error document.
  # Only 404 is rewritten, never 403. A 403 from the ad server means "this
  # tracking token is forged or expired" and must reach the client intact --
  # rewriting it would hide the integrity check doing its job.
  custom_error_response {
    error_code            = 404
    response_code         = 404
    response_page_path    = "/index.html"
    error_caching_min_ttl = 10
  }

  restrictions {
    geo_restriction { restriction_type = "none" }
  }

  viewer_certificate {
    cloudfront_default_certificate = var.enable_custom_domain ? false : true
    acm_certificate_arn            = var.enable_custom_domain ? aws_acm_certificate_validation.main[0].certificate_arn : null
    ssl_support_method             = var.enable_custom_domain ? "sni-only" : null
    minimum_protocol_version       = var.enable_custom_domain ? "TLSv1.2_2021" : null
  }
}
