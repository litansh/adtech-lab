-- Latency, because in AdTech it is eligibility rather than comfort.
--
-- Past a buyer's tmax you do not lose the auction -- you were never in it.
-- Phase 1 has no external buyers, so this is the baseline we will be held to
-- when Phase 4 introduces one with a real deadline.

SELECT
    dt,
    COUNT(*)                                                     AS ad_requests,
    ROUND(APPROX_PERCENTILE(latency_ms, 0.50), 3)                AS p50_ms,
    ROUND(APPROX_PERCENTILE(latency_ms, 0.95), 3)                AS p95_ms,
    ROUND(APPROX_PERCENTILE(latency_ms, 0.99), 3)                AS p99_ms,
    ROUND(MAX(latency_ms), 3)                                    AS max_ms
FROM adtech_lab.events
WHERE dt BETWEEN '{{FROM}}' AND '{{TO}}'
  AND event = 'ad_request'
GROUP BY dt
ORDER BY dt;
