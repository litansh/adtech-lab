-- The four fill metrics, kept deliberately distinct.
--
-- There is no universal definition of "fill rate" -- platforms disagree about
-- the denominator, and that disagreement is a real source of cross-platform
-- reporting arguments. So we name which one we mean, every time.
--
--   request_fill_rate  responses carrying an ad / ad requests
--   render_rate        impressions / responses carrying an ad
--   ctr                clicks / impressions
--
-- The gap between request_fill_rate and render_rate is the interesting one: it
-- is ads we decided to serve that never actually rendered.

WITH d AS (
    SELECT
        COUNT_IF(event = 'ad_request')                                    AS ad_requests,
        COUNT_IF(event = 'ad_request' AND decision_reason = 'selected')   AS responses_with_ad,
        COUNT_IF(event = 'ad_impression')                                 AS impressions,
        COUNT_IF(event = 'ad_click')                                      AS clicks
    FROM adtech_lab.events
    WHERE dt BETWEEN '{{FROM}}' AND '{{TO}}'
)
SELECT
    ad_requests,
    responses_with_ad,
    impressions,
    clicks,
    ROUND(100.0 * responses_with_ad / NULLIF(ad_requests, 0), 2)      AS request_fill_rate_pct,
    ROUND(100.0 * impressions       / NULLIF(responses_with_ad, 0), 2) AS render_rate_pct,
    ROUND(100.0 * clicks            / NULLIF(impressions, 0), 3)       AS ctr_pct
FROM d;
