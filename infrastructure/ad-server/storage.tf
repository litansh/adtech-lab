# ---------------------------------------------------------------------------
# Control plane + serving state.
#
# On-demand only: scale-to-zero, no provisioned capacity to forget about.
# Control-plane items are cached in Lambda memory with a short TTL, so the hot
# path issues ~zero reads. See docs/architecture-proposal.md.
# ---------------------------------------------------------------------------
resource "aws_dynamodb_table" "control_plane" {
  name         = "adtech-lab-control-plane"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  range_key    = "sk"

  attribute {
    name = "pk"
    type = "S"
  }
  attribute {
    name = "sk"
    type = "S"
  }

  point_in_time_recovery { enabled = false } # campaign config is reproducible from fixtures
}

# serving_budget_state. Deliberately NOT the billing ledger: approximate is
# acceptable here, bounded over-delivery is expected, and the durable record is
# derived from the event stream instead.
resource "aws_dynamodb_table" "serving_state" {
  name         = "adtech-lab-serving-state"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"

  attribute {
    name = "pk"
    type = "S"
  }

  # Counters are daily and worthless afterwards. TTL keeps the table from
  # growing without bound and costs nothing.
  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }
}

# ---------------------------------------------------------------------------
# Data plane: the billing ledger's source of truth.
# ---------------------------------------------------------------------------
resource "aws_s3_bucket" "events" {
  bucket = "adtech-lab-events-${var.account_id}"
}

resource "aws_s3_bucket_public_access_block" "events" {
  bucket                  = aws_s3_bucket.events.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "events" {
  bucket = aws_s3_bucket.events.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "events" {
  bucket = aws_s3_bucket.events.id
  rule {
    id     = "raw-events-retention"
    status = "Enabled"
    filter {}

    # docs/privacy-baseline.md commits to: "raw event data retained 90 days,
    # then aggregated and the raw records deleted."
    #
    # This rule used to transition to GLACIER_IR at 90 days and expire at 365,
    # so raw records outlived the stated commitment by four times. A privacy
    # commitment the infrastructure does not honour is worse than no commitment,
    # because it is relied upon.
    #
    # The transition is also removed rather than moved. Glacier IR bills a
    # 128KB MINIMUM per object against Standard's actual size, so it is only
    # cheaper above ~22KB:
    #
    #     standard    = 0.023 * size_kb
    #     glacier_ir  = 0.004 * max(size_kb, 128)
    #     break-even  = 0.512 / 0.023 = 22.3 KB
    #
    # Our objects are one Lambda invocation's buffer of gzipped NDJSON -- far
    # below that -- so the transition was costing several times what it saved,
    # plus a per-object transition fee. Small objects are the case where cold
    # storage classes lose.
    expiration { days = 90 }

    # An interrupted multipart upload is invisible and billable forever.
    abort_incomplete_multipart_upload { days_after_initiation = 7 }
  }
}
