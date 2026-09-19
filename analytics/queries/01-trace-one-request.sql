-- "Why did this user see this ad?"
--
-- The Phase 1 traceability goal, in one query. Give it a request_id and it
-- returns that request's whole life: the decision, the impression, the click.
--
-- Always filter on dt (and hh when you can): partition projection means an
-- unfiltered query scans every day we have ever recorded, and Athena bills by
-- bytes scanned. The 1GB per-query cutoff on the workgroup is the backstop.

SELECT
    event,
    from_unixtime(ts / 1000)          AS at,
    decision_reason,
    decision_rule,
    line_item_id,
    creative_id,
    expected_ecpm,
    candidates_evaluated,
    latency_ms,
    advertiser_spend,
    country,
    device_type,
    game
FROM adtech_lab.events
WHERE dt = '{{DATE}}'
  AND request_id = '{{REQUEST_ID}}'
ORDER BY ts;
