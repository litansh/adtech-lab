# AWS Cost Model

Pricing: **us-east-1**, verified August 2026. Update on any material architecture change.

> **Correction from the first draft.** Three errors materially changed these numbers: DynamoDB on-demand writes were quoted at the pre-November-2024 rate; the browser request path was modelled as 2 CloudFront requests per ad request when it is closer to 3-6; and the architecture now uses API Gateway. The corrected cost per 1M ad requests is roughly **3x** the first estimate.

## Corrected Unit Prices

| Service | Price |
|---|---|
| Lambda requests | $0.20 / 1M |
| Lambda duration (ARM64) | $0.0000133334 / GB-second |
| **API Gateway HTTP API** | **$1.00 / 1M** (first 300M/month) |
| CloudFront requests, pay-as-you-go (NA) | $0.0100 / 10,000 = $1.00 / 1M — *first 10M/month always free* |
| CloudFront data out (NA/EU) | $0.085/GB — *first 1TB/month always free* |
| CloudFront flat-rate plans | ❌ **UNAVAILABLE** — AWS Free Tier accounts cannot use them. Verified against account <account-id> on 2026-08-28 |
| AWS WAF (if added) | $5/mo per Web ACL + $1/rule + $0.60/M requests ≈ **$7-8/mo** — **deferred**, see below |
| **DynamoDB on-demand write** | **$0.625 / 1M WRU** (was incorrectly stated as $1.25) |
| **DynamoDB on-demand read** | **$0.125 / 1M RRU** (an eventually-consistent read consumes 0.5 RRU) |
| Amazon Data Firehose | $0.029/GB, **each record rounded up to 5KB** |
| S3 Standard | $0.023/GB-month; PUT $0.005/1,000 |
| Athena | $5.00 / TB scanned |
| CloudWatch Logs ingest | $0.50/GB (Standard class) |
| Route 53 hosted zone | $0.50/month standalone (**included** in a flat-rate plan) |
| ACM, IAM, Budgets, Cost Anomaly Detection | $0 |

## Request Multipliers — The Actual Browser Path

One "ad request" is not one HTTP request. The real path, per page view with one ad slot:

| Browser request | Cacheable | CloudFront requests | Dynamic (API GW + Lambda) |
|---|---|---|---|
| Page HTML | Edge-cacheable | 1.0 | 0 |
| `adtag.js` | Long TTL, browser-cached | 0.2 – 1.0 | 0 |
| CSS / favicon | Long TTL | 0.2 – 1.0 | 0 |
| `GET /ad` (ad request) | Never cached | 1.0 | 1.0 |
| Creative image | Long TTL, browser-cached | 0.15 – 0.6 | 0 |
| `GET /event/impression` | Never cached | 0.6 | 0.6 |
| `GET /event/viewable` (Phase 2) | Never cached | 0.6 | 0.6 |
| `GET /event/click` | Never cached | 0.004 | 0.004 |
| `POST /collect` (product analytics, **batched**) | Never cached | 1.3 | 1.3 |

Assumptions: 60% fill rate, 0.6% CTR, one slot per page view.

```text
Model A  (effective browser/edge caching, Phase 2 events)   ~5.1 CloudFront  |  ~3.5 dynamic
Model B  (conservative, little caching benefit)             ~7.1 CloudFront  |  ~3.5 dynamic
```

**Product analytics is why the dynamic figure moved from 2.2 to 3.5.** The games emit ~13 product events per session. Sent individually that would be ~8.9 extra dynamic requests per ad request and would roughly triple this line. They are therefore **batched** — accumulated in memory, flushed on `visibilitychange` and unload via `sendBeacon`, capped at 50 events / 32KB per batch — reducing them to ~2 HTTP requests per session, or ~1.3 per ad request. Batching product telemetry is not an optimisation here; without it, product analytics would dominate both the cost model and the Cost Fuse signal.

**Static-vs-dynamic is the crucial split.** Cached static requests still cost a CloudFront request but never reach the origin. Only the ~2.2 dynamic requests per ad request drive API Gateway, Lambda, DynamoDB and Firehose. All scenarios below use **Model B (conservative)**; Model A reduces the CloudFront line by roughly 35%.

Data transfer: ~40KB per ad request (page + assets + creative + responses).

### What the mini-games Publisher changes

The multiplier model holds, with two adjustments that push in opposite directions:

* **Caching improves.** One shared `styles.css`, one `game-room.js` and one `adtag.js` are reused across `/`, `/xo` and `/connect-four`, and a games site gets repeat visits. Model A (~3.8 CloudFront requests per ad request) is the realistic case, not the optimistic one.
* **Ad requests per session fall.** A player loads `/xo` once and then replays for ten minutes without a page reload. An article site would have generated five page views — and five ad requests — in the same span.

That second point is a genuine Publisher-economics finding, not an accounting detail:

> **High engagement and high ad-opportunity volume are not the same thing, and can be in direct tension.** A game that holds attention on one page view produces fewer ad opportunities than a site that fragments attention across many. This is exactly the pressure that produces slideshow galleries, infinite scroll and aggressive ad refresh — and exactly the pressure the Product Quality Rule exists to resist. We will be able to measure both sides of it: `session_duration` against `ad_opportunities_per_session`.

Practical effect on the cost model: at a given number of **sessions**, a games Publisher generates fewer ad requests than a content Publisher — so the scenarios below, which are indexed on ad requests, remain valid and simply correspond to more sessions.

### Per-session request baseline

```text
games_per_session        4        page_views_per_session   1.5

  A  POST /ad/request                 1.5/session
  B  GET  /event/*                    0.9/session (P1)  |  1.8/session (P2)
  C  POST /collect (batched)          2.0/session       carries ~13 product events
                                     -----
     total dynamic                    4.4/session (P1)  |  5.3/session (P2)
```

The three route groups are metered and fused **separately** — see the Cost Fuse in [architecture-proposal.md](architecture-proposal.md). Product analytics must never be able to trip the advertising fuse.

---

## Scenarios

### Scenario 0 — Idle (zero traffic)

| Item | $/month |
|---|---|
| CloudFront (no traffic) | 0.00 |
| Route 53 hosted zone for xoxoxo.live | 0.50 |
| S3 (site + creatives, ~200MB) | 0.01 |
| DynamoDB, Lambda, API GW, Firehose (no requests) | 0.00 |
| Domain `xoxoxo.live` — registered at GoDaddy, **not an AWS cost** | 0.00 |
| **Total** | **≈ $0.51** |

ACM certificates are free. The domain is registered externally, so AWS idle cost is just the Route 53 hosted zone.

### Scenario 1 — 100K ad requests/month

`710K CloudFront requests · 350K dynamic requests · 4GB transfer`

| Item | $/month |
|---|---|
| CloudFront (710K requests, 4GB — inside the 10M req / 1TB always-free tier) | 0.00 |
| API Gateway HTTP API (350K) | 0.35 |
| Lambda (350K invocations + 380 GB-s) | 0.08 |
| DynamoDB (60K WRU) | 0.04 |
| Firehose (350K records x 5KB = 1.8GB) | 0.05 |
| CloudWatch Logs (0.09GB) | 0.05 |
| S3 + Athena | 0.02 |
| Domain | 1.17 |
| **Total** | **≈ $1.76** |

### Scenario 2 — 1M ad requests/month

`7.1M CloudFront requests · 3.5M dynamic requests · 40GB transfer`

| Item | $/month |
|---|---|
| API Gateway HTTP API (3.5M) | 3.50 |
| Lambda (3.5M invocations + 4,000 GB-s) | 0.75 |
| DynamoDB (1.2M WRU — budget + lab frequency counters) | 0.75 |
| Firehose (3.5M records x 5KB = 17.5GB) | 0.51 |
| CloudWatch Logs (0.9GB, unsampled) | 0.45 |
| S3 + Athena | 0.06 |
| Domain | 1.17 |
| **Subtotal excluding CloudFront** | **7.19** |
| **+ CloudFront** (7.1M requests < 10M always-free tier) | 0.00 |
| **Total** | **≈ $7.19** |

At this volume pay-as-you-go is cheaper because the always-free 10M request tier still covers us — but it carries overage risk and no bundled WAF. The Pro plan buys a genuine hard ceiling for $15. **Decision point, not a foregone conclusion.**

### Scenario 3 — 10M ad requests/month

`71M CloudFront requests · 35M dynamic requests · 400GB transfer`

| Item | Unoptimized | Optimized |
|---|---|---|
| CloudFront requests (PAYG, 10M free) | 61.00 | 41.00 *(Model A caching)* |
| AWS WAF (separate, on PAYG) | 8.00 | 8.00 |
| API Gateway HTTP API (35M) | 35.00 | 35.00 |
| Lambda (35M invocations + 40,000 GB-s) | 7.53 | 7.53 |
| DynamoDB (12M WRU) | 7.50 | 3.75 *(batched flush)* |
| Firehose (35M records) | 5.08 | 0.51 *(10:1 record batching)* |
| CloudWatch Logs (8.8GB) | 4.40 | 0.04 *(1% sampling)* |
| S3 + Athena | 0.60 | 0.60 |
| Domain | 1.17 | 1.17 |
| **Total** | **≈ $130** | **≈ $97** |

🛑 **10M ad requests/month now exceeds the $100 ceiling even optimized.** Adding product analytics moved this scenario from ~$98/~$63 to ~$130/~$97. Reaching this volume requires an explicit approval conversation and, realistically, a design change — most likely moving product analytics off the synchronous request path (client-side aggregation, or CloudFront real-time logs instead of a collection endpoint). A CloudFront flat-rate Business plan ($200/month) is not viable under the ceiling.

For scale: 10M ad requests/month corresponds to roughly **6.7M sessions/month**, about 220,000 sessions/day. That is far beyond anything Phase 1-2 will produce, and by the time it is a real question we will have measured numbers rather than estimates.

### Scenario 4 — 100M ad requests/month

Not modelled in detail. It exceeds the ceiling by a wide margin and is out of scope for this project without real revenue and explicit approval.

---

## Unit Economics

| Metric | Unoptimized | Optimized |
|---|---|---|
| **Per 1M ad requests** (beyond free tiers, incl. product analytics) | **≈ $13.00** | **≈ $9.70** |
| **Per 1M tracking events** | ≈ $1.80 | ≈ $1.50 |
| **Per impression served** (60% fill) | ≈ $0.0000217 | ≈ $0.0000162 |
| Storage (1M events, gzipped, per month) | ≈ $0.004 | — |
| Analytics (one month-wide Athena query) | ≈ $0.002 | — |
| Data transfer | $0 up to 1TB/month | — |

At a 15% take rate on a $2.00 CPM, marginal fee per impression is ~$0.0003 against a marginal infrastructure cost of ~$0.0000162 — roughly **1:19**. Still a high-margin shape, but each correction has moved it the same way: 1:70 in the first draft, 1:27 in the second, 1:19 now. **Every cost the early model omitted was real, and none of the omissions worked against the estimate.** The ratio degrades further as fill rate falls, because unfilled requests cost compute and earn nothing.

---

## Cost Safety — What Actually Bounds Spend

### Reserved concurrency is not a spending limit

The first draft called `Lambda reserved concurrency = 20` "the hard ceiling on the account." **That is wrong.** Reserved concurrency caps *simultaneous executions*, not cost.

```text
20 concurrent executions / 0.015s average duration = ~1,333 requests/second
                                                    = ~3.4 billion requests/month
```

Concurrency bounds throughput. On its own it would permit a bill in the thousands of dollars. It is one control among several, not the ceiling.

### The layered control set

| Layer | Control | What it actually bounds |
|---|---|---|
| Edge | CloudFront flat-rate plan | **Hard: no overage charges, ever.** WAF-blocked requests do not count toward the allowance |
| Edge | WAF IP-based rate limiting (included in the plan) | Requests per IP per 5-minute window |
| Edge | Geographic blocking | Traffic from regions we do not serve |
| API | API Gateway route throttling (rate + burst) | Requests/second reaching the origin — **the binding constraint on origin cost** |
| Compute | Lambda reserved concurrency | Blast radius and downstream write pressure |
| Application | Validate and reject before any DynamoDB write or Firehose record | Per-request downstream cost |
| **Project** | **Cost Fuse** — traffic-volume alarms trip a fail-closed block on the dynamic ad endpoints | **Duration.** Bounds how long any abuse can bill, independent of how fast it is |
| Account | AWS Budgets ($20/$40/$60/$80/$100), Cost Anomaly Detection | **Detection, not prevention** — billing data lags hours to a day |

### Worked abuse scenario

An attacker discovers `/ad` and floods it for a full month, undetected.

**On pay-as-you-go CloudFront, API Gateway throttled to 50 rps:**

```text
binding constraint: 50 rps x 2.63M seconds = 131M requests/month

CloudFront requests   131M - 10M free = 121M x $1.00/M   = $121
API Gateway           131M x $1.00/M                     = $131
Lambda                131M x $0.20/M + short durations    =  $27
DynamoDB              rejected before write               =   $0
Firehose              1% sampled                          =   $0.19
CloudWatch            sampled                             =   $1
Data out              26GB, within the 1TB free tier      =   $0
                                                          -------
                                            worst case    = ~$280/month
```

Tightening the API Gateway throttle to **20 rps** brings this to **~$107/month**.

**On a CloudFront flat-rate plan, same attack:**

```text
WAF IP rate limiting blocks the flood.
Blocked requests never count toward the allowance and are never billed.
CloudFront cost                                 = $0 (Free) or $15 (Pro), fixed
Origin cost for traffic that gets past WAF      = ~$14
                                                  -------
                                    worst case  = ~$15-30/month
```

### No contractual cap is available — what that changes

The account is on the **AWS Free plan**, which cannot use CloudFront flat-rate pricing plans. The "no overage charges, regardless of traffic spikes or attacks" guarantee that earlier drafts relied on **is not available to us**. Verified live against account <account-id> on 2026-08-28.

Consequences:

1. **There is no contractual cost cap anywhere in this stack.** Every service bills by usage.
2. **The Cost Fuse is now the primary control**, not a secondary one.
3. **WAF is deferred.** At $7-8/month it costs more than the entire projected bill ($1-7/month). API Gateway route throttling, Lambda reserved concurrency and the Cost Fuse cover Phase 1. WAF gets added the moment there is real traffic or an actual abuse event — not before.
4. CloudFront's **always-free tier still applies**: 10M requests and 1TB egress per month, permanently. At Phase 1-2 volumes we never leave it.

### The Cost Fuse bounds duration

The controls above bound a *rate*. The **Cost Fuse** (designed in [architecture-proposal.md](architecture-proposal.md)) bounds *duration*. It is **route-aware**: separate alarms and fuses for ad serving (`/ad/request`), advertising measurement (`/event/*`), and product analytics (`/collect`), so a popular Publisher can never stop ad serving.

```text
Expected fuse response time
  1 min metric period + ~1 min alarm evaluation + ~2s Lambda   = ~2-3 min   [ESTIMATE]
Traffic in that window, bounded by the 20 rps route throttle   = ~4,800 requests
Estimated cost of one trip event                               = ~$0.02     [ESTIMATE]
```

### Estimated worst-case monthly exposure

Every figure is an **estimate** from documented service behaviour. None has been measured.

| Scenario | Estimated cost | Notes |
|---|---|---|
| Normal Phase 1 operation | **$0.51 - $2** | Route 53 zone plus negligible usage |
| One abuse event on a dynamic route, fuse trips | **+$0.02** | Per event |
| Persistent attacker, 20 trip/reset cycles | **+$0.40** | Before adding a WAF IP block |
| Fast fuse fails; daily fuse catches it after 24h at 20 rps | **+$3** | 1.73M dynamic requests |
| Analytics flood, Analytics Fuse trips | **+$0.02**, ad serving unaffected | The route split makes this true |
| **Static-path flood** (games site, bypasses the dynamic fuses) | **up to +$90** | ⚠️ **the largest uncapped risk** — see below |
| **Estimated worst case, fuses functioning** | **≈ $5 - $15/month** | |
| All fuse tiers and the daily manual check fail, full month at 20 rps | **≈ $107/month** | Estimated architectural worst case |

### ⚠️ The static-path flood is now the biggest uncapped risk

Without a flat-rate plan and without WAF, a flood against `/`, `/xo` or the static assets is served by CloudFront and billed per request once past the 10M/month free tier. 100M requests would be roughly $90, and the Cost Fuse does **not** cover it — those routes are deliberately excluded so a traffic spike cannot take the games offline.

What actually limits it:

* **Cost Anomaly Detection at IMMEDIATE frequency, $5 threshold** — deployed. Against a $0.51/month baseline, $5 is a loud signal that arrives in minutes, not days.
* **Budget alerts at $20 / $40 / $60 / $80 / $100** — deployed.
* **Response:** add a WAF rate-based rule at that point. It costs $7-8/month and stops being an unjustifiable expense the moment it is actually needed.

This is a deliberate, stated trade-off: we accept a bounded tail risk on static paths in exchange for not paying $7-8/month indefinitely to insure a $0.51/month system. The detection layer is what makes it acceptable.

### Deployed guardrails (2026-08-28)

```text
adtech-lab-monthly-actual     $100 budget, alerts at 20/40/60/80/100%
adtech-lab-monthly-forecast   $100 budget, forecast alert
adtech-lab-services           Cost Anomaly monitor, DIMENSIONAL by SERVICE
adtech-lab-anomalies          IMMEDIATE frequency, >= $5 impact
adtech-lab-cost-alerts        SNS topic -> <alert-email>
```

Terraform: `infrastructure/guardrails/`, local state, `allowed_account_ids = ["<account-id>"]`.
**Not yet applied, pending explicit approval:** the budget freeze action at 80%.

### Account plan constraints

```text
accountPlanType:             FREE
accountPlanRemainingCredits: $119.94
accountPlanExpirationDate:   2027-02-08
```

At $1-7/month, credits comfortably cover Phases 1-5. **The expiry date, not the money, is the binding constraint.** Phase 6+ requires a decision on upgrading well before February 2027.

### Estimates, not ceilings

> **There is no hard monetary ceiling on this stack. AWS provides none for usage-based services, and the one contractual cap that exists (CloudFront flat-rate) is unavailable on the Free plan.**

* The **design goal** — estimated worst-case exposure below $100/month — is met by layered controls, not by a guarantee.
* Every latency and cost figure is an **estimate** assuming the throttle holds, the alarm fires on the first breaching datapoint, and the fuse Lambda succeeds first try.
* **Residual risk** is correlated control failure, plus the static-path tail above.

### Replacing estimates with measurements

After Phase 1 deployment, a deliberate bounded load test against our own endpoints replaces these with **measured** values:

| To measure | Currently |
|---|---|
| Actual fuse response time, alarm fire to endpoints blocked | Estimated ~2-3 min |
| Requests through before the block takes effect | Estimated ~4,800 |
| Actual billed cost of one trip event | Estimated ~$0.02 |
| Whether the route throttle holds at its configured rate | Assumed |
| Games stay online throughout | Asserted, untested |
| Analytics Fuse trips without affecting ad serving | Asserted, untested |

A safety control nobody has fired is a hypothesis.

## Optimization Levers

1. **Aggressive browser caching** of `adtag.js` and creatives (immutable filenames, long `max-age`) — the difference between Model A and Model B, roughly 35% of the CloudFront line.
2. **Batched counter flush** in Lambda memory — trades DynamoDB WRUs against bounded over-delivery. This is the real trade-off every ad server makes; see `architecture-proposal.md`.
3. **Log sampling** — 1% of ordinary requests, 100% of errors and lab/debug requests.
4. **Firehose record batching** — the 5KB rounding means ten 400-byte events cost the same as one record. ~10x saving on that line.
5. **Short retention** — CloudWatch 7 days; S3 lifecycle to Glacier IR at 90 days.
6. **Run load tests against the origin directly**, bypassing CloudFront, so synthetic traffic does not consume a plan allowance and latency measurements are cleaner.

## Cost Traps — Explicit Review

| Trap | Status |
|---|---|
| NAT Gateway | No VPC in Phase 1. Note: Lambda in a VPC does **not** by itself require NAT — see `architecture-proposal.md` |
| Data transfer | Within the 1TB free tier or a plan allowance at all modelled volumes |
| CloudWatch ingestion | 7-day retention + sampling. Unsampled logging is a visible line item at 10M/month |
| Athena scan volume | Hourly partitions; Parquet conversion in Phase 2 |
| Unbounded Lambda concurrency | Reserved concurrency 20 — for blast radius, **not** as a cost ceiling |
| High-frequency scheduled jobs | None in Phase 1 |
| Provisioned databases | DynamoDB on-demand only |
| Cross-region | Single region |
| Large S3 storage | Lifecycle policy |
| Public abuse / DDoS (dynamic routes) | Route-aware Cost Fuse + API Gateway throttling + Lambda concurrency |
| Public abuse / DDoS (static routes) | ⚠️ Uncapped. Bounded by IMMEDIATE anomaly alerts at $5 and budget alerts; response is to add WAF |
| No contractual billing cap available | Free plan blocks CloudFront flat-rate plans. Accepted; mitigated by layered controls |

## Guardrails Before the First Deployment

```text
CloudFront pay-as-you-go (flat-rate plans unavailable on the Free plan)
WAF deferred until real traffic or an abuse event ($7-8/mo vs a $0.51/mo baseline)
API Gateway route throttling: 20 rps steady / 40 burst
Lambda reserved concurrency: 20
DynamoDB on-demand only
Cost allocation tags: Project, Environment, Component
AWS Budgets: $20 / $40 / $60 / $80 / $100 -> email
Cost Anomaly Detection on the project tag
CloudWatch retention: 7 days
S3 lifecycle: 90d -> Glacier IR, 365d -> delete
Manual cost check daily during active development

Cost Fuse (route-aware; per-route API Gateway metrics):

  AD FUSE   -> WAF block /ad* + /event*, ad-server reserved concurrency -> 0
    warn      A  POST /ad/request   > 60/min       -> email only
    warn      B  GET  /event/*      > 60/min       -> email only
    fast      A  POST /ad/request   > 300/min      -> TRIP
    fast      B  GET  /event/*      > 300/min      -> TRIP
    daily     A  POST /ad/request   > 40,000/day   -> TRIP
    daily     B  GET  /event/*      > 50,000/day   -> TRIP
    backstop  AWS Budgets actual spend > $60       -> TRIP

  ANALYTICS FUSE -> WAF block /collect only; ad serving UNAFFECTED
    warn      C  POST /collect      > 100/min      -> email only
    fast      C  POST /collect      > 600/min      -> TRIP (analytics only)
    daily     C  POST /collect      > 100,000/day  -> TRIP (analytics only)

  alert only  CloudFront Requests > 100,000/hr     -> email; never trips
  recovery    manual: adlabctl fuse reset --ad | --analytics
```

No automatic account-wide destructive shutdown without explicit approval.

## Account Prerequisites — RESOLVED 2026-08-28

1. ✅ Account `<account-id>` is on the **Free plan**. CloudFront flat-rate plans are therefore **unavailable**. Edge design reverted to pay-as-you-go with WAF deferred.
2. ✅ Credits $119.94, expiry 2027-02-08. Sufficient for Phases 1-5; the date is the binding constraint.
3. ✅ Domain `xoxoxo.live` registered at GoDaddy — `route53domains` is unusable on the Free plan anyway. Route 53 hosted zone plus NS delegation in Phase 1.

## Sources

- [DynamoDB on-demand pricing](https://aws.amazon.com/dynamodb/pricing/on-demand/) · [Lambda pricing](https://aws.amazon.com/lambda/pricing/) · [API Gateway pricing](https://aws.amazon.com/api-gateway/pricing/) · [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) · [Data Firehose pricing](https://aws.amazon.com/firehose/pricing/)
- [CloudFront pay-as-you-go pricing and always-free tier](https://aws.amazon.com/cloudfront/pricing/pay-as-you-go/)
- [CloudFront flat-rate pricing plans — features, allowances, what counts toward usage](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/flat-rate-pricing-plan.html) · [Plan tiers and account requirements](https://docs.aws.amazon.com/PricingPlanManager/latest/UserGuide/plans.html)
- [API Gateway HTTP API throttling](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-throttling.html)
- [AWS Free Tier changes, July 2025](https://aws.amazon.com/about-aws/whats-new/2025/07/aws-free-tier-credits-month-free-plan/)
