-- Unfilled inventory earns nothing, so the reasons matter more than the rate.
--
-- This is the aggregate view of the Decision Trace: every rejection reason,
-- counted. A high geo_mismatch means the demand does not match the audience;
-- a high budget_exhausted means demand is there but capped; no_eligible_
-- candidates means we simply have nothing to sell.

SELECT
    decision_reason,
    COUNT(*)                                                        AS requests,
    ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2)              AS pct_of_requests,
    ROUND(AVG(latency_ms), 3)                                       AS avg_latency_ms,
    ROUND(AVG(CAST(candidates_evaluated AS DOUBLE)), 1)             AS avg_candidates
FROM adtech_lab.events
WHERE dt BETWEEN '{{FROM}}' AND '{{TO}}'
  AND event = 'ad_request'
GROUP BY decision_reason
ORDER BY requests DESC;
