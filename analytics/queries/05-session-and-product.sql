-- Publisher economics: is the product any good, and what does monetising it
-- cost in engagement?
--
-- This is the query that makes the monetisation-versus-experience trade-off
-- visible. Raising ad frequency raises impressions_per_session -- but if it
-- shortens sessions, revenue_per_session can FALL while impressions rise.
-- That is the number to watch, not impressions.
--
-- Note the join. Impression and click events carry request_id but NOT
-- session_id, so revenue has to be attributed back through the ad_request that
-- produced it. That is not an oversight to paper over: it is how real systems
-- reconstruct a session, because the beacon that fires on render knows about
-- the ad, not about the visit.
--
-- returning_users is deliberately absent: session_id dies with the tab and
-- there is no persistent identifier. Measuring it is a gated decision, not a
-- query. See docs/privacy-baseline.md.

WITH req AS (        -- request_id -> session_id, the bridge between the two worlds
    SELECT request_id, MAX(session_id) AS session_id
    FROM adtech_lab.events
    WHERE dt BETWEEN '{{FROM}}' AND '{{TO}}'
      AND event = 'ad_request' AND session_id IS NOT NULL
    GROUP BY request_id
),
money AS (           -- spend, attributed back to the session that generated it
    SELECT r.session_id, SUM(COALESCE(e.advertiser_spend, 0)) AS gross_spend
    FROM adtech_lab.events e
    JOIN req r ON e.request_id = r.request_id
    WHERE e.dt BETWEEN '{{FROM}}' AND '{{TO}}'
      AND e.event IN ('ad_impression', 'ad_click')
    GROUP BY r.session_id
),
product AS (
    SELECT
        session_id,
        MIN(COALESCE(ts, received_ts))    AS first_ts,
        MAX(COALESCE(ts, received_ts))    AS last_ts,
        COUNT_IF(event = 'game_start')    AS games_started,
        COUNT_IF(event = 'game_end')      AS games_completed,
        COUNT_IF(event = 'game_replay')   AS replays,
        COUNT_IF(event = 'ad_request')    AS ad_opportunities
    FROM adtech_lab.events
    WHERE dt BETWEEN '{{FROM}}' AND '{{TO}}'
      AND session_id IS NOT NULL
    GROUP BY session_id
)
SELECT
    COUNT(*)                                                                 AS sessions,
    ROUND(AVG((p.last_ts - p.first_ts) / 1000.0), 1)                         AS avg_session_seconds,
    ROUND(AVG(p.games_started), 2)                                           AS games_per_session,
    ROUND(100.0 * SUM(p.games_completed) / NULLIF(SUM(p.games_started), 0), 1) AS completion_rate_pct,
    ROUND(100.0 * SUM(p.replays) / NULLIF(SUM(p.games_completed), 0), 1)     AS replay_rate_pct,
    ROUND(AVG(p.ad_opportunities), 2)                                        AS ad_opportunities_per_session,
    ROUND(SUM(COALESCE(m.gross_spend, 0)) / NULLIF(COUNT(*), 0), 6)          AS revenue_per_session,
    ROUND(1000.0 * SUM(COALESCE(m.gross_spend, 0)) / NULLIF(COUNT(*), 0), 4) AS publisher_rpm_per_1000_sessions
FROM product p
LEFT JOIN money m ON m.session_id = p.session_id;
