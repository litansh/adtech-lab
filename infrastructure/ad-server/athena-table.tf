# ---------------------------------------------------------------------------
# The events table.
#
# One wide table over a heterogeneous event stream, with every column nullable.
# That is the normal shape for an event log: ad_request, ad_impression,
# ad_click, game_start and game_end all land in the same place, and a query
# filters on `event`. Splitting them into separate tables would make the
# interesting questions -- which join ad events to product events on session_id
# -- unnecessarily hard.
#
# Partition PROJECTION rather than a partition catalogue: Athena computes the
# partitions from the key pattern, so nothing has to run MSCK REPAIR or a
# crawler on a schedule. No Glue crawler means no crawler bill.
# ---------------------------------------------------------------------------
resource "aws_glue_catalog_table" "events" {
  name          = "events"
  database_name = aws_glue_catalog_database.events.name
  table_type    = "EXTERNAL_TABLE"

  parameters = {
    EXTERNAL                      = "TRUE"
    "projection.enabled"          = "true"
    "projection.dt.type"          = "date"
    "projection.dt.format"        = "yyyy-MM-dd"
    "projection.dt.range"         = "2026-08-01,NOW"
    "projection.dt.interval"      = "1"
    "projection.dt.interval.unit" = "DAYS"
    "projection.hh.type"          = "integer"
    "projection.hh.range"         = "0,23"
    "projection.hh.digits"        = "2"
    "storage.location.template"   = "s3://${aws_s3_bucket.events.bucket}/events/dt=$${dt}/hh=$${hh}/"
    classification                = "json"
  }

  partition_keys {
    name = "dt"
    type = "string"
  }
  partition_keys {
    name = "hh"
    type = "string"
  }

  storage_descriptor {
    location      = "s3://${aws_s3_bucket.events.bucket}/events/"
    input_format  = "org.apache.hadoop.mapred.TextInputFormat"
    output_format = "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat"

    ser_de_info {
      serialization_library = "org.openx.data.jsonserde.JsonSerDe"
      parameters = {
        "ignore.malformed.json" = "true"
      }
    }

    # --- identity and routing ---
    columns {
      name = "event"
      type = "string"
    }
    columns {
      name = "event_id"
      type = "string"
    }
    columns {
      name = "request_id"
      type = "string"
    }
    columns {
      name = "session_id"
      type = "string"
    }
    columns {
      name = "ts"
      type = "bigint"
    }
    columns {
      name = "received_ts"
      type = "bigint"
    }
    # --- inventory ---
    columns {
      name = "placement_id"
      type = "string"
    }
    columns {
      name = "site_id"
      type = "string"
    }
    columns {
      name = "game"
      type = "string"
    }
    # --- request context: deliberately coarse. No IP, no raw user agent. ---
    columns {
      name = "country"
      type = "string"
    }
    columns {
      name = "device_type"
      type = "string"
    }
    columns {
      name = "referrer_class"
      type = "string"
    }
    # --- decisioning ---
    columns {
      name = "decision_reason"
      type = "string"
    }
    columns {
      name = "decision_rule"
      type = "string"
    }
    columns {
      name = "candidates_evaluated"
      type = "int"
    }
    columns {
      name = "latency_ms"
      type = "double"
    }
    columns {
      name = "line_item_id"
      type = "string"
    }
    columns {
      name = "campaign_id"
      type = "string"
    }
    columns {
      name = "creative_id"
      type = "string"
    }
    columns {
      name = "expected_ecpm"
      type = "double"
    }
    # --- money ---
    columns {
      name = "advertiser_spend"
      type = "double"
    }
    # --- product events ---
    columns {
      name = "mode"
      type = "string"
    }
    columns {
      name = "level"
      type = "int"
    }
    columns {
      name = "outcome"
      type = "string"
    }
    columns {
      name = "winner"
      type = "string"
    }
    columns {
      name = "moves"
      type = "int"
    }
    columns {
      name = "game_session_id"
      type = "string"
    }
    columns {
      name = "prev_game_session_id"
      type = "string"
    }
    columns {
      name = "from_game"
      type = "string"
    }
    columns {
      name = "to_game"
      type = "string"
    }
  }
}
