# The Publisher: A Mini-Games Site

## Why a games site rather than a content site

The Publisher is not a prop. It is the second half of the learning objective — we are learning **AdTech economics** and **Publisher economics**, and the second is impossible to learn on a site nobody wants to visit.

| Property | Why it matters for learning |
|---|---|
| Real user value | The site must be worth visiting with the ads removed. Anything else teaches us MFA economics, which is not a skill worth acquiring |
| Repeat visits | Retention and return rate are Publisher metrics that a one-off article cannot produce |
| Longer sessions | Session-level metrics (games per session, session RPM) only exist if sessions exist |
| Natural ad opportunities | A game has real structural boundaries — start, end, replay, switch — so we can study *where* an ad opportunity legitimately belongs |
| Measurable engagement | Game completion and replay rate give us a genuine product KPI to trade against monetisation |
| Plausible traffic path later | Organic or paid acquisition for a games site is legitimate; for a content farm it is not |

## The Product Quality Rule

> **The site must provide real value even if advertising is removed entirely.**

This is a hard rule, not an aspiration. Concretely, in Phase 1:

* One ad placement per game page. No interstitials, no refresh, no ads on the home page.
* The ad sits **below** the game, never overlapping or crowding it.
* If the ad server is unavailable, the slot collapses and the game is unaffected.
* We will explicitly test that the site is good with ads disabled — and the Cost Fuse (see [architecture-proposal.md](architecture-proposal.md)) makes that the literal failure mode: when it trips, ad serving stops and **the games keep working**.

What we are not building: an MFA site, an ad-refresh machine, fake engagement, or pages that exist to display ads.

---

## Phase 1 Scope: One Good Game

```text
/                    game room home
  /xo                Tic-Tac-Toe        <- Game #1, Phase 1
  /connect-four      Connect Four       <- already exists; ship if free, else Phase 2
```

Explicitly **not** in Phase 1 or 2 without a product reason: accounts, multiplayer backend, global leaderboards, chat, social features, a mobile app, a game engine, ten games, or a React/Next.js rewrite.

The mini-games product must not become a reason to delay the AdTech work. Phase 1 is:

```text
one good game  +  one real ad placement  +  one real ad server  +  complete traceability
```

## Ad Placement — Phase 1

```text
+-----------------------------------+
|                                   |
|            GAME BOARD             |
|                                   |
+-----------------------------------+
|   scores / controls / difficulty  |
+-----------------------------------+

+-----------------------------------+
|   AD PLACEMENT  300x250           |   placement_id = game_sidebar
|   one request per page view       |   no refresh
+-----------------------------------+
```

**Candidate future ad opportunities** — to be evaluated, not assumed:

| Opportunity | Argument for | Argument against |
|---|---|---|
| Game completion | A natural pause; high attention | Interrupts the replay loop, which is the retention driver |
| Between rounds | Structural boundary | Frequency escalates fast in a game with 30-second rounds |
| Game switch | Genuine navigation moment | Rare event, low volume |
| Refresh on a timer | More impressions | Directly degrades viewability quality and user experience |

> **We do not create an ad opportunity because we technically can.** Each one is a hypothesis to be tested against product metrics, not a feature to ship.

## The Advertising UX Guardrail

Whenever we change monetisation, we measure the product effect in the same experiment. Never one without the other.

| If we change | We must observe |
|---|---|
| More placements | session_duration, games_per_session, replay_rate |
| Interstitials | game_completion_rate, exit rate, return rate |
| Ad refresh | viewability_rate, replay_rate, session_duration |
| Larger or stickier formats | game_completion_rate, bounce |
| Higher ad frequency | all of the above |

The question is never *"how many impressions can we generate?"* It is:

> **What level of monetisation maximises long-term value without degrading the product?**

That is the core Publisher yield lesson, and it cannot be learned from a site nobody uses.

---

## Event Model

Phase 1 does not need dashboards. It **does** need event names that will not have to be renamed later. Two families, joined by `session_id`.

### Product events

```text
session_start   { session_id, ts, device_type, country, referrer_class }
game_view       { session_id, game, ts }
game_start      { session_id, game, game_session_id, mode, level }
game_end        { session_id, game, game_session_id, outcome, duration_ms, moves }
game_replay     { session_id, game, prev_game_session_id }
game_switch     { session_id, from_game, to_game }
```

**Transport: batched.** `track()` accumulates events in memory and flushes on `visibilitychange` and on unload via `sendBeacon`, capped at **50 events or 32KB per batch**, to `POST /collect`. This turns ~13 product events per session into ~2 HTTP requests. Batching is not a micro-optimisation here — sent individually, product events would outnumber ad requests roughly 9 to 1 and would dominate both the cost model and the Cost Fuse signal. `/collect` is metered and fused **separately** from the advertising routes, precisely so that a popular Publisher can never stop ad serving.

`game_move` is deliberately **not** emitted in Phase 1 — it is high volume, and its cost is real (see the Firehose line in [cost-model.md](cost-model.md)). If we need move-level analysis later, it will be sampled.

### Ad events

```text
ad_request      { request_id, session_id, placement_id, game, device_type, country }
ad_decision     { request_id, selected_line_item_id | decision_reason,
                  candidates_evaluated, latency_ms }
ad_impression   { request_id, event_id, line_item_id, creative_id }
ad_click        { request_id, event_id, line_item_id, creative_id }
ad_viewable     { request_id, event_id }                       # Phase 2
```

### `session_id` and privacy — stated precisely

A `session_id` **is** an identifier, so it needs the same discipline as any other:

```text
Scope           one browsing session in one tab
Storage         in-memory, or sessionStorage at most
Lifetime        dies when the tab closes; never restored
Persistence     NOT written to a cookie or localStorage
Used for        joining product events to ad events within one visit
NOT used for    ad targeting, frequency capping, or cross-visit recognition
Disclosed in    the privacy notice
```

This keeps Phase 1 consistent with [privacy-baseline.md](privacy-baseline.md): **no persistent advertising identifier.**

**The honest consequence:** `returning_users` and cross-visit retention **cannot be measured** under this model. That is a real limitation, not an oversight. Measuring it requires a persistent first-party identifier, which is a gated decision with a written purpose, a notice update, and a consent analysis — exactly the gate described in the privacy baseline. Experiencing that trade-off is part of the lesson.

## Publisher Metrics — what the event model enables

| Metric | Derivation | Available in Phase 1? |
|---|---|---|
| `sessions` | count of `session_start` | Yes |
| `games_started` | count of `game_start` | Yes |
| `games_completed` | count of `game_end` where outcome is terminal | Yes |
| `games_per_session` | `game_start / sessions` | Yes |
| `replay_rate` | `game_replay / game_end` | Yes |
| `session_duration` | last event ts − `session_start` ts | Yes (approximate) |
| `ad_opportunities_per_session` | `ad_request / sessions` | Yes |
| `impressions_per_session` | `ad_impression / sessions` | Yes |
| `revenue_per_session` | publisher revenue / sessions | Yes (virtual money) |
| `publisher_RPM` | publisher revenue / sessions × 1000, denominator stated | Yes |
| `traffic_acquisition_cost` | manual entry; zero in Phase 1 | Yes |
| `publisher_contribution_profit` | revenue − TAC − operating cost | Yes |
| `returning_users` | — | **No.** Requires a persistent identifier |

Phase 1 builds the events and one Athena query. Dashboards are Phase 2.

---

## Two P&Ls, Never Merged

Both entities are ours. They are still different businesses, and merging them destroys the lesson.

### Publisher economics

```text
  advertising_revenue              credited by our ad server to the publisher entity
- traffic_acquisition_cost         paid acquisition; $0 in Phase 1
- publisher_operating_cost         hosting attributable to the site, content/game work
= publisher_contribution_profit
```

### AdTech platform economics

```text
  gross_media_handled              total value transacted through the platform
- publisher_payout                 owed onward to the publisher entity
= platform_fee                     platform_fee = media_amount * take_rate
- infrastructure_cost              AWS
- vendor_cost                      third parties, when any exist
= platform_contribution_profit
```

The interesting question this structure lets us ask, which a merged P&L cannot: *at what take rate does the platform become profitable, and at what take rate does the publisher stop being viable?* With one site and one platform we can see both sides of that negotiation at once — which is a view almost nobody in the industry actually gets.

## Sources

- [MRC viewability guidelines (via IAB Tech Lab)](https://iabtechlab.com/standards/open-measurement-sdk/)
