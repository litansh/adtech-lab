-- FOLLOW ONE DOLLAR.
--
-- Two separate P&Ls. They belong to the same person here, and they are still
-- different businesses: a Publisher can be profitable while the platform
-- serving it loses money, and the reverse. Merging them destroys the lesson.
--
--   publisher_payout   owed onward to the Publisher entity
--   platform_fee       what the platform retains  = media x take_rate
--
-- Note what this does NOT claim: advertiser_spend is an EXPENSE to the
-- advertiser, and it is not anyone's revenue. Only the payout and the fee are.

WITH take AS (SELECT 0.15 AS take_rate),           -- platform take rate
     spend AS (
    SELECT
        line_item_id,
        COUNT_IF(event = 'ad_impression')          AS impressions,
        COUNT_IF(event = 'ad_click')               AS clicks,
        SUM(COALESCE(advertiser_spend, 0))         AS gross_advertiser_spend
    FROM adtech_lab.events
    WHERE dt BETWEEN '{{FROM}}' AND '{{TO}}'
      AND event IN ('ad_impression', 'ad_click')
    GROUP BY line_item_id
)
SELECT
    s.line_item_id,
    s.impressions,
    s.clicks,
    ROUND(s.gross_advertiser_spend, 6)                                  AS gross_advertiser_spend,
    ROUND(s.gross_advertiser_spend * (1 - t.take_rate), 6)              AS publisher_payout,
    ROUND(s.gross_advertiser_spend * t.take_rate, 6)                    AS platform_fee,
    -- eCPM realised, as opposed to the eCPM we expected at decision time
    ROUND(1000.0 * s.gross_advertiser_spend / NULLIF(s.impressions, 0), 4) AS realised_ecpm
FROM spend s CROSS JOIN take t
ORDER BY gross_advertiser_spend DESC;
