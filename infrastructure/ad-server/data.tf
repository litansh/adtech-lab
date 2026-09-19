# ---------------------------------------------------------------------------
# Lambda deployment package.
#
# Built by `make build` in services/ad-server: GOOS=linux GOARCH=arm64.
# ARM64 (Graviton) is ~20% cheaper per GB-second than x86 and cold-starts fast
# for a static Go binary.
# ---------------------------------------------------------------------------
data "archive_file" "ad_server" {
  type        = "zip"
  source_file = "${path.module}/../../services/ad-server/build/bootstrap"
  output_path = "${path.module}/build/ad-server.zip"
}

data "archive_file" "cost_fuse" {
  type        = "zip"
  source_file = "${path.module}/../../services/cost-fuse/build/bootstrap"
  output_path = "${path.module}/build/cost-fuse.zip"
}

resource "random_password" "token_key" {
  length  = 48
  special = false
}
