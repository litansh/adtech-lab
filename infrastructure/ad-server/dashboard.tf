# ---------------------------------------------------------------------------
# Observability.
#
# There are TWO different problems here and they want different tools:
#
#   OPERATIONAL  latency, errors, throughput, cost   -> CloudWatch (this file)
#   BUSINESS     fill rate, eCPM, RPM, session RPM   -> event stream -> Athena
#
# Business metrics do NOT belong in a metrics system. Their cardinality is
# unbounded -- line_item x creative x placement x country x game x device --
# and a time-series database charges per series. Real ad platforms do not put
# fill rate in Prometheus; they put it in a query layer over the event log.
# That is what Athena is for here.
#
# CloudWatch gives 3 dashboards free, which is 3 more than we need.
# ---------------------------------------------------------------------------
resource "aws_cloudwatch_dashboard" "main" {
  dashboard_name = "adtech-lab"

  dashboard_body = jsonencode({
    widgets = [
      {
        type = "text", x = 0, y = 0, width = 24, height = 2
        properties = {
          markdown = "# adtech-lab\n**Operational** signals only. Business metrics (fill rate, eCPM, RPM, session RPM) come from the event stream via Athena -- see `docs/cost-model.md` for why they do not belong in a metrics system."
        }
      },
      {
        # Traffic by route: this is the Cost Fuse's own signal, so seeing it
        # here is how you build intuition for whether the thresholds are sane.
        type = "metric", x = 0, y = 2, width = 12, height = 6
        properties = {
          title  = "Requests by route (Cost Fuse signal)"
          region = var.region
          stat   = "Sum"
          period = 300
          metrics = [
            for k, r in local.routes :
            ["AWS/ApiGateway", "Count", "ApiId", aws_apigatewayv2_api.main.id, "Stage", "$default", "Route", r, "Resource", r, "Method", "-", { label = k }]
          ]
          annotations = {
            horizontal = [
              { label = "ad fuse trip (300/min = 1500/5min)", value = 1500 },
              { label = "warn (60/min)", value = 300 }
            ]
          }
        }
      },
      {
        # Latency. In AdTech this is eligibility, not comfort: past tmax you do
        # not lose the auction, you were never in it.
        type = "metric", x = 12, y = 2, width = 12, height = 6
        properties = {
          title  = "Ad server latency (p50 / p95 / p99)"
          region = var.region
          period = 300
          metrics = [
            ["AWS/Lambda", "Duration", "FunctionName", aws_lambda_function.ad_server.function_name, { stat = "p50", label = "p50" }],
            ["...", { stat = "p95", label = "p95" }],
            ["...", { stat = "p99", label = "p99" }]
          ]
        }
      },
      {
        type = "metric", x = 0, y = 8, width = 8, height = 6
        properties = {
          title  = "Errors and throttles"
          region = var.region
          stat   = "Sum"
          period = 300
          metrics = [
            ["AWS/Lambda", "Errors", "FunctionName", aws_lambda_function.ad_server.function_name],
            [".", "Throttles", ".", "."],
            ["AWS/ApiGateway", "5xx", "ApiId", aws_apigatewayv2_api.main.id],
            [".", "4xx", ".", "."]
          ]
        }
      },
      {
        # Concurrency against the reservation. If this pins at 20, the fuse is
        # about to matter.
        type = "metric", x = 8, y = 8, width = 8, height = 6
        properties = {
          title  = "Concurrency (account limit: 10)"
          region = var.region
          period = 60
          metrics = [
            ["AWS/Lambda", "ConcurrentExecutions", "FunctionName", aws_lambda_function.ad_server.function_name, { stat = "Maximum" }]
          ]
          annotations = { horizontal = [{ label = "account limit", value = 10 }] }
        }
      },
      {
        # The cost drivers, in one place: DynamoDB writes and Lambda invocations.
        type = "metric", x = 16, y = 8, width = 8, height = 6
        properties = {
          title  = "Cost drivers: DDB writes, Lambda invocations"
          region = var.region
          stat   = "Sum"
          period = 300
          metrics = [
            ["AWS/DynamoDB", "ConsumedWriteCapacityUnits", "TableName", aws_dynamodb_table.serving_state.name],
            ["AWS/Lambda", "Invocations", "FunctionName", aws_lambda_function.ad_server.function_name]
          ]
        }
      }
    ]
  })
}
