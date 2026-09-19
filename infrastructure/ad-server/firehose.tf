# ---------------------------------------------------------------------------
# Event pipeline.
#
# This was Amazon Data Firehose. It is NOT available on the AWS Free account
# plan -- SubscriptionRequiredException in every region -- so the ad server
# writes gzipped NDJSON straight to S3 instead, batched per invocation.
#
# Same hourly partition layout Firehose would have produced, so the Athena
# table and every query survive unchanged if we ever move to a managed
# pipeline. Cost trade-off documented in services/ad-server/sink_s3.go and
# docs/cost-model.md: fine at Phase 1 volumes, needs real buffering at ~1M
# requests/month.
# ---------------------------------------------------------------------------

# ---------------------------------------------------------------------------
# Athena: pay-per-scan, no warehouse to keep running.
# ---------------------------------------------------------------------------
resource "aws_s3_bucket" "athena_results" {
  bucket = "adtech-lab-athena-${var.account_id}"
}

resource "aws_s3_bucket_public_access_block" "athena_results" {
  bucket                  = aws_s3_bucket.athena_results.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_lifecycle_configuration" "athena_results" {
  bucket = aws_s3_bucket.athena_results.id
  rule {
    id     = "expire-query-results"
    status = "Enabled"
    filter {}
    expiration { days = 14 }
  }
}

resource "aws_athena_workgroup" "main" {
  name = "adtech-lab"
  configuration {
    result_configuration { output_location = "s3://${aws_s3_bucket.athena_results.bucket}/results/" }
    # A hard stop against a runaway scan. The Cost Fuse cannot see Athena.
    bytes_scanned_cutoff_per_query = 1073741824 # 1 GB
  }
  force_destroy = true
}

resource "aws_glue_catalog_database" "events" {
  name = "adtech_lab"
}
