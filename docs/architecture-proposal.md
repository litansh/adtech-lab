# Architecture Proposal — Phase 1

## The Goal of Phase 1

One complete vertical slice that actually runs, on a Publisher that is a real product:

```text
Real user
   -> Mini-games Publisher  (/xo)
   -> Game page renders, ad slot mounts
   -> adtag.js  ->  POST /ad/request
   -> Ad Server: eligibility -> selection -> Decision Trace
   -> House Campaign creative
   -> Render -> Impression -> Event pipeline -> Reporting
```

**No auction between independent buyers. No SSP. No DSP. No OpenRTB. No external monetisation.**
Demand in Phase 1 is **House / manually configured campaigns**. See [publisher-product.md](publisher-product.md) for the Publisher itself.

### Phase 1 vocabulary

Because there is no bidder in Phase 1, we do not use bidding vocabulary. Using it early would teach the wrong mental model.

| Do **not** use in Phase 1 | Use instead | Meaning |
|---|---|---|
| `bid` | `line_item_rate` | The configured rate on the line item (CPM/CPC/CPA) |
| `bid price`, `auction price` | `value_cpm` | The rate normalised to a CPM |
| `highest bid` | `expected_ecpm` | Value per 1,000 impressions after applying expected CTR/CVR |
| `auction winner` | `selected_line_item` | The outcome of delivery decisioning |
| `clearing price` | — | Has no meaning without competing independent buyers |
| — | `priority`, `selection_score` | The inputs to the decision |

`bid`, `BidRequest`, `auction`, `winner` and `clearing_price` enter the codebase in **Phase 3**, when independent buyers do.

---

## The Architecture

```text
                        Route 53  (adlab.example)
                             |
                    CloudFront distribution
              (pay-as-you-go, Shield Standard, ACM TLS;
               WAF deferred until real traffic)
                             |
        +--------------------+--------------------+
        |                                         |
  /  , /static/*, /creatives/*            /ad, /event/*
  (cacheable, long TTL)                   (dynamic, no-store)
        |                                         |
        v                                         v
   S3 (private, OAC)                    API Gateway HTTP API
   - index.html (game room)             (route-level throttling)
   - xo/index.html                                |
   - static/game-room.<hash>.js                   v
   - static/styles.<hash>.css            Lambda: ad-server (Go, ARM64)
   - static/adtag.<hash>.js
   - creatives/*
   - ads.txt (placeholder form)
                                                  |
                        +-------------------------+-------------------+
                        |                         |                   |
                        v                         v                   v
                 DynamoDB (on-demand)      Amazon Data Firehose   CloudWatch Logs
                 - control plane cache            |                (7d, sampled)
                 - serving_budget_state           v
                                            S3 events/  ->  Athena
                                            (NDJSON.gz, hourly partitions)
                                            = billing_ledger source
```

## Component Choices

| Purpose | Service | Why | Alternative considered |
|---|---|---|---|
| Edge, TLS, DDoS, DNS | **CloudFront pay-as-you-go** + ACM + Route 53 | 10M requests / 1TB egress always free; Shield Standard included. Flat-rate plans are unavailable on the Free plan | Flat-rate plan (ineligible); WAF (deferred, $7-8/mo) |
| Static assets, creatives, ads.txt | S3 + OAC | Cacheable, cheap, simple | — |
| Dynamic endpoints | **API Gateway HTTP API** | Native POST, per-route throttling as a real cost control, standard access logs | Lambda Function URL + OAC (see the analysis below) |
| Ad server | Lambda (Go, ARM64, 128MB) | Scale-to-zero, low cold start, cheap per GB-second | ECS Fargate (fixed monthly cost) |
| Control plane + serving counters | DynamoDB on-demand | Scale-to-zero, low latency, no provisioning | RDS (fixed monthly cost) |
| Event pipeline | Amazon Data Firehose -> S3 | Serverless, per-GB, no shards to manage | Kinesis Data Streams; Kafka/MSK |
| Analytics | S3 + Athena | Pay-per-scan | Redshift, ClickHouse |
| Cost guardrails | AWS Budgets, Cost Anomaly Detection | Free | — |

### CloudFront: pay-as-you-go, WAF deferred

Earlier drafts recommended a **CloudFront flat-rate pricing plan** for its "no overage charges, regardless of traffic spikes or attacks" guarantee. **That option is not available to us.** AWS documents that Free Tier accounts cannot use flat-rate plans, and account `<account-id>` is on the Free plan — verified live on 2026-08-28.

So the edge is **pay-as-you-go**:

| | Value |
|---|---|
| Always-free tier | **10M requests + 1TB egress per month, permanently** |
| Beyond that | $1.00 per 1M requests (NA), $0.085/GB egress |
| DDoS protection | Shield Standard, included at no cost |
| WAF | **Deferred.** $5/mo per Web ACL + $1/rule + $0.60/M requests ≈ $7-8/mo |

**Why WAF is deferred:** the projected bill is $0.51-7/month. Paying $7-8/month for WAF would more than double the cost of the entire system to insure against an event that has not happened. Phase 1 relies on API Gateway route throttling, Lambda reserved concurrency and the Cost Fuse. **WAF gets added the moment there is real traffic or an actual abuse event** — at which point it stops being an unjustifiable expense.

**The honest consequence:** there is now **no contractual cost cap anywhere in this stack**, and a flood against the *static* routes bypasses the Cost Fuse by design (those routes are excluded so a traffic spike cannot take the games offline). That tail risk is bounded by detection rather than prevention — Cost Anomaly Detection at IMMEDIATE frequency with a $5 threshold, plus budget alerts from $20. Against a $0.51/month baseline, $5 is a loud and fast signal. See [cost-model.md](cost-model.md).

### API Gateway HTTP API vs Lambda Function URL + OAC

I recommended Function URL + OAC in the first draft, primarily to avoid the $1.00/M API Gateway charge. On review, that was optimising the wrong variable.

**The POST problem.** With CloudFront OAC in front of a Lambda Function URL configured as `AuthType: AWS_IAM`, Lambda does not accept unsigned payloads. For `POST`/`PUT`, the **viewer** must compute the SHA-256 of the request body and send it in the `x-amz-content-sha256` header. In a browser ad tag that means using `crypto.subtle.digest` on every request, adding an async step and a failure mode, for no functional benefit. The common workarounds — a Lambda@Edge origin-request function to re-sign, or dropping to `AuthType: NONE` — either add a component or remove the protection that justified OAC in the first place.

**What API Gateway HTTP API buys us:**

| | Function URL + OAC | API Gateway HTTP API |
|---|---|---|
| GET endpoints | Fine | Fine |
| POST from a browser | Requires viewer-computed payload hash | Works normally |
| POST server-to-server (OpenRTB, Phase 5) | Requires SigV4 signing by the caller | Works normally |
| **Per-route throttling (rate + burst)** | **Not available** | **Built in** — a real cost control |
| Access logs, stages | Roll your own | Standard |
| Cost | $0 | $1.00 per 1M requests (first 300M/month) |

**Recommendation: API Gateway HTTP API.** The per-route throttle is a genuine safety control (see the abuse analysis below), POST works without gymnastics, and Phase 5's OpenRTB endpoint will need POST. The cost is roughly **$1.61 per 1M ad requests** at our request multiplier — cents at Phase 1-2 volumes, ~$16/month at 10M ad requests/month. That is the right trade for simplicity and for a control we actually need.

### Correcting an error from the first draft: VPC and NAT

The first draft said "putting Lambda in a VPC requires a NAT Gateway." **That is wrong.**

```text
Lambda CAN run inside a VPC with no NAT Gateway.

A NAT Gateway (or another egress mechanism such as a VPC endpoint,
an egress-only gateway, or a self-managed NAT instance) is required only
when a workload in a private subnet needs outbound internet connectivity.
```

Lambda in a VPC reaching only VPC-internal resources, or reaching AWS services through VPC endpoints, needs no NAT. The reason we avoid a VPC in Phase 1 is simply that **we have no private resource to reach** — not that a VPC is inherently expensive. If we ever add one, NAT cost becomes a question to answer then, not an axiom.

---

## Data Model

### Control plane
Publisher, Site, Placement, Advertiser, Campaign, Line Item, Creative, Targeting, Budget configuration.
Stored in DynamoDB, **loaded into Lambda memory with a short TTL**. Real ad servers push configuration snapshots to serving nodes for the same reason: a per-request database lookup does not fit the latency budget. Consequence: near-zero DynamoDB reads on the hot path.

### Two different kinds of "spend" — and they are not the same system

The first draft claimed serving state "must be exact." That is not how real systems work, and pretending otherwise hides the actual engineering trade-off.

```text
serving_budget_state                     billing_ledger
--------------------                     --------------
purpose: delivery control                purpose: money
consistency: approximate is acceptable   consistency: durable and reconcilable
latency: must fit the request budget     latency: batch is fine
implementation: counters, may be          implementation: append-only event
  sharded, cached, or batched-flushed       records with idempotency keys
tolerates: bounded over-delivery          tolerates: nothing
source of truth for: "may this line       source of truth for: "what is owed
  item still serve right now?"              to whom"
```

Real ad servers accept **bounded over-delivery** — a campaign may spend slightly past its cap because many serving nodes are counting concurrently and reconciling asynchronously. The publisher/advertiser contract accounts for this. What is *not* negotiable is that the billing ledger is durable, idempotent, and reconcilable after the fact.

In our implementation:
* `serving_budget_state` — DynamoDB atomic counters, optionally batched in Lambda memory and flushed. Tuning the flush interval is a deliberate experiment: it trades DynamoDB cost against over-delivery.
* `billing_ledger` — derived from the Firehose -> S3 event stream, each event carrying a unique `event_id` so replays are idempotent. Reconciled by an Athena query, not by reading counters.

Teaching them as one thing would be the single most misleading simplification we could make.

---

## Delivery Decisioning — More Than eCPM

The first draft said the ad server "ranks everything by eCPM." That is too simple to be true.

Delivery decisioning combines, roughly in this order:

```text
1. eligibility        Is this line item able to serve this opportunity at all?
                      - flight dates, status, budget remaining
                      - a creative matching the slot's size and format
                      - targeting predicates satisfied (geo, device, context, inventory)
                      - frequency state permits it
2. priority           Guaranteed/sponsorship demand generally outranks
                      non-guaranteed demand regardless of price
3. delivery need      A guaranteed line item behind schedule is favoured;
                      one ahead of schedule is throttled (pacing)
4. economic value     expected_ecpm, normalised across pricing models
5. opportunity cost   Serving this impression here means not serving it elsewhere
```

**Targeting is part of determining eligibility, not a universally separate pipeline stage.** Different platforms structure this differently; some evaluate targeting as a predicate inside candidate selection, others as an indexed pre-filter. We will implement it as part of eligibility and say so.

**A concrete industry example of why price alone is not the rule:** Google Ad Manager's **dynamic allocation** lets real-time demand (AdX/AdSense) compete against *remnant* line items on price, while **guaranteed** line items are protected by priority and delivery obligations. A $15 real-time bid does not automatically beat a $12 guaranteed sponsorship, because the publisher has contracted to deliver that sponsorship. Opportunity cost and contractual obligation sit above raw eCPM.

---

## Request Traceability

Every event carries:

```text
request_id      ULID generated at ingress; returned to the client always
opportunity_id  identifies the slot within the request
placement_id, site_id, publisher_id
line_item_id, campaign_id, creative_id, advertiser_id
decision_reason enum, e.g. selected | no_eligible_candidates | budget_exhausted
event_id        unique per emitted event, for idempotent ledger replay
ts              UTC milliseconds
```

Target: answer *"why did this request produce this creative?"* with one Athena query.

## Decision Trace — Access Control

The Decision Trace is the highest-value learning feature, and it exposes campaign configuration, budget state and internal reasoning. It is **not** a public endpoint.

```text
Lab environment       full trace available on request
Production            full trace NOT returned to the browser
Production response   always includes request_id
Investigation path    request_id -> Athena / structured logs -> full trace
```

**Explicitly rejected:** `?debug=1` as the only gate, and any long-lived shared secret embedded in `adtag.js`. Anything shipped to a browser is public. If we ever want trace access against production, it goes through an authenticated admin path with a short-lived credential, not a query parameter.

## Tracking Integrity — from Phase 1

Measurement integrity is an AdTech concept, not an afterthought. If any URL can mint a valid impression, every metric we build on top is fiction — and we would be teaching ourselves the exact failure mode that IVT exploits.

Phase 1 design, kept deliberately simple:

* **Signed, expiring tracking tokens.** The ad response embeds a token — an HMAC over `(request_id, line_item_id, creative_id, exp)` using a key held only server-side. `/event/impression` and `/event/click` reject anything that fails verification or has expired. This is a simplified version of what real ad servers do; the key rotation story is the part we are skipping for now.
* **Idempotent events.** Each event carries a unique `event_id`. Replays are recorded once in the billing ledger.
* **The click endpoint is not an open redirect.** The destination URL comes from the creative record on the server, looked up by `creative_id` from the verified token — never from a URL parameter supplied by the caller.
* **Basic sanity filtering** at ingress: method, content type, obviously non-browser user agents. Real GIVT/SIVT detection is a Phase 2+ simulation topic.

## Privacy Baseline

Phase 1 production uses **no persistent advertising identifier**. See [privacy-baseline.md](privacy-baseline.md) for the full baseline and for how frequency capping is handled without one.

---

## Infrastructure vs Business Objects

**Terraform manages infrastructure only.**

```text
Terraform:   CloudFront, S3, API Gateway, Lambda, DynamoDB tables, Firehose,
             IAM roles, Athena workgroup, Budgets, alarms
NOT Terraform: Publishers, Sites, Placements, Advertisers, Campaigns,
             Line Items, Creatives, Targeting, Budgets
```

Business objects are managed by a small **`adlabctl` seed CLI** writing to DynamoDB, with JSON fixture files kept in the repo. Reasons: business objects change constantly, `terraform destroy` must never be able to delete campaign data, and campaign state is not desired-state configuration. In Phase 2 the same operations move behind an authenticated internal admin API.

## Technology Choice

| Component | Language | Why |
|---|---|---|
| ad-server | **Go** | Low cold start on ARM64, cheap per GB-second, strong typing for the OpenRTB structs we will need in Phase 5 |
| adtag.js, publisher page | **TypeScript**, no framework | Browser. An ad tag should stay small |
| simulation and analysis | **Python** | Offline only |
| IaC | **Terraform** | — |

**Honest trade-off:** at Phase 1-2 volumes Node.js or Python would work fine and ship faster. Go is chosen because building latency intuition is an explicit learning objective, and because the OpenRTB work in Phase 5 benefits from real structs.

## Proposed Repo Structure

```text
adtech-lab/
  docs/
    adr/                     # architecture decision records
  infrastructure/
    guardrails/              # budgets, anomaly detection        [deployed]
    publisher/               # S3 + CloudFront for the games site
    ad-server/               # API Gateway, Lambda, DynamoDB, Firehose, Cost Fuse
  services/
    ad-server/               # Go
    cost-fuse/               # Go, small; trips the fuse on alarm
  apps/publisher/            # game room: HTML + game-room.js + adlab.js + adtag.js
  tools/adlabctl/            # seed CLI + `fuse status` / `fuse reset`
  CLAUDE.md
```

**Separate Terraform stacks are deliberate**, not tidiness. `apps/publisher` is a
different economic entity from the platform, and it splits into its own
repository at Phase 7 when a second Publisher exists — see
[ADR 0001](adr/0001-publisher-lives-in-the-monorepo-until-phase-7.md). Keeping
the stacks and deploy paths separate now makes that split a `git filter-repo`
rather than an untangling. `apps/publisher` may never import from `services/`
or `packages/`; it reaches the ad server over HTTP, exactly like an external
Publisher would.

Nothing else until it is needed. The existing `xo-game` project is **copied** into `apps/publisher` — never moved, never edited in place. The original stays untouched as the pristine source.

---

## The Cost Fuse

### Why the previous answer was not good enough

The last revision claimed a "~$30/month architectural maximum." That claim does not hold. A CloudFront flat-rate plan contractually caps **CloudFront**. It caps nothing else. API Gateway, Lambda, DynamoDB, Firehose and CloudWatch all remain usage-based, and API Gateway throttling is a safety control that bounds a *rate* — it is not a financial limit and it can be misconfigured, raised, or bypassed on a route that was forgotten.

**AWS provides a genuine hard monetary ceiling for exactly one component in this stack: CloudFront under a flat-rate plan. Nothing else.** So we build our own fail-closed mechanism.

### Objective

> If abnormal or abusive traffic occurs, ad serving must **fail closed** long before AWS spend can approach the $100/month project ceiling — and the games must keep working.

Not in scope, ever: shutting down the AWS account, or deleting infrastructure.

### Design

```text
                Traffic
                   |
                   v
          CloudFront + WAF                     [static paths unaffected]
                   |
                   v
          API Gateway (20 rps throttle)
                   |
                   v
          Lambda: ad-server
                   |
        emits metrics to CloudWatch
                   |
                   v
   +---------------------------------+
   |  CloudWatch alarms (4 tiers)    |
   |   warn / fast / daily / budget  |
   +---------------------------------+
                   |
                   v
              SNS topic  ------------------> email to me
                   |
                   v
          Lambda: cost-fuse
             1. PutFunctionConcurrency(ad-server, 0)
             2. Enable the WAF BLOCK rule on /ad* and /event*
             3. Write TRIPPED state + reason to DynamoDB
             4. Publish alert with the metric that fired
                   |
                   v
          Dynamic ad endpoints blocked
          Static game site still online
                   |
                   v
          Manual investigation
                   |
                   v
          adlabctl fuse reset      (deliberate, never automatic)
```

### Why these two actions, together

| Action | Stops | Does not stop |
|---|---|---|
| **WAF block rule on `/ad*`, `/event*`** | CloudFront origin fetch, API Gateway, Lambda, DynamoDB, Firehose. On a flat-rate plan, **blocked requests are not billed and do not count toward the allowance** | The CloudFront request charge on pay-as-you-go |
| **`PutFunctionConcurrency(ad-server, 0)`** | Every Lambda execution and therefore every downstream write. Throttled invocations are not billed as Lambda duration | API Gateway and CloudFront request charges |

Neither is sufficient alone. WAF is the cheaper block; concurrency-zero is the one that cannot be routed around, because it acts at the Lambda service boundary regardless of how the request arrived. Defence in depth, because the whole point is that individual controls fail.

### Route groups — why one undifferentiated alarm was wrong

The previous threshold arithmetic counted only advertising traffic. The Publisher now emits product events (`game_start`, `game_end`, `game_replay`, ...), and those outnumber ad requests by roughly **9 to 1**. A single alarm on total API Gateway `Count` would therefore be dominated by product analytics — meaning **the site becoming popular would trip the advertising cost fuse.** That is a failure mode, not a safety control.

The fuse is therefore split by route group, using API Gateway per-route metrics (`Count` dimensioned by `ApiId, Stage, Route`):

| Group | Routes | Purpose | Trips which fuse |
|---|---|---|---|
| **A** | `POST /ad/request` | Ad serving | **Ad Fuse** |
| **B** | `GET /event/impression`, `/event/click`, `/event/viewable` | Advertising measurement | **Ad Fuse** |
| **C** | `POST /collect` | Product analytics (batched) | **Analytics Fuse** — never the Ad Fuse |

Groups A and B are kept as separate alarms rather than summed, because the difference is diagnostic: a flood of `/event/*` **without** matching `/ad/request` volume is not a cost attack, it is attempted measurement fraud. Different problem, different response.

**Product analytics is batched.** `track()` accumulates events in memory and flushes on `visibilitychange` and on unload via `sendBeacon`, capped at **50 events or 32KB per batch** (enforced at the API Gateway payload limit and re-checked in the handler). This turns ~13 product events per session into ~2 HTTP requests, and it is what keeps product telemetry from dominating both the cost model and the fuse signal.

### Traffic baseline, recalculated per session

```text
Planning baseline          500 sessions/day
Growth planning case     5,000 sessions/day   (10x)
games_per_session            4
page_views_per_session       1.5

Per session, by route group:
  A  POST /ad/request                    1.5    (one per page view, no refresh)
  B  GET  /event/*                       0.9    Phase 1   |  1.8  Phase 2 (+viewable)
  C  POST /collect  (batched)            2.0    carries ~13 product events
                                        -----
     total dynamic requests              4.4    Phase 1   |  5.3  Phase 2
```

| Group | At 500 sessions/day | At 5,000 sessions/day |
|---|---|---|
| A `/ad/request` | 750/day · 0.52/min | 7,500/day · 5.2/min |
| B `/event/*` | 900/day · 0.63/min | 9,000/day · 6.25/min |
| C `/collect` | 1,000/day · 0.70/min | 10,000/day · 7.0/min |

> Worth noting: the previously proposed single daily threshold of 50,000/day would have been **exceeded by legitimate product analytics alone** at the 10x-growth case had events been sent unbatched (67,000/day). The route split plus batching removes that trap.

### The tiers

**Ad Fuse** — any of these trips it:

| Tier | Signal | Period | Threshold | Multiple of the 10x case | Action |
|---|---|---|---|---|---|
| Warn | A `Count` | 1 min | > 60 | ~12x | Email only |
| Warn | B `Count` | 1 min | > 60 | ~10x | Email only |
| **Fast** | A `Count` | 1 min | **> 300** | ~58x | **TRIP** |
| **Fast** | B `Count` | 1 min | **> 300** | ~48x | **TRIP** |
| **Daily** | A `Count` | 1 day | **> 40,000** | ~5.3x | **TRIP** |
| **Daily** | B `Count` | 1 day | **> 50,000** | ~5.6x | **TRIP** |
| **Backstop** | AWS Budgets actual spend | — | **$60** | — | **TRIP** |

**Analytics Fuse** — independent, and it *cannot* stop ad serving:

| Tier | Signal | Period | Threshold | Multiple of the 10x case | Action |
|---|---|---|---|---|---|
| Warn | C `Count` | 1 min | > 100 | ~14x | Email only |
| **Fast** | C `Count` | 1 min | **> 600** | ~86x | **TRIP (analytics only)** |
| **Daily** | C `Count` | 1 day | **> 100,000** | ~10x | **TRIP (analytics only)** |

Alert-only, never trips: CloudFront `Requests > 100,000/hour` — static traffic is capped by the flat-rate plan, and blocking it would take the games offline for what may be a legitimate spike.

All fast thresholds sit below the API Gateway route throttle (20 rps = 1,200/min), so the metrics can actually reach them.

### Actions, by fuse

| | Ad Fuse | Analytics Fuse |
|---|---|---|
| WAF block rule | `/ad*` and `/event*` | `/collect` only |
| `PutFunctionConcurrency(ad-server, 0)` | **Yes** | **No** |
| Ad serving | Stopped | **Unaffected** |
| Product analytics | Stopped as collateral | Stopped |
| Games | **Online** | **Online** |
| Reset | `adlabctl fuse reset --ad` | `adlabctl fuse reset --analytics` |

The Ad Fuse setting concurrency to zero also stops `/collect`, since both routes share one Lambda. That is accepted collateral: when the Ad Fuse trips we want the whole dynamic path closed, and the games are static and keep working regardless. The reverse is explicitly **not** true — an analytics flood must never take ad serving down, which is why the Analytics Fuse touches only its own WAF rule.

### Expected fuse response time

These are **estimates derived from documented service behaviour, not measured values.**

```text
Expected fuse response time
  CloudWatch metric period                        1 min
  + alarm evaluation and state transition        ~1 min
  + SNS delivery and Lambda execution            ~2 s
  ------------------------------------------------------
  expected                                       ~2-3 minutes
```

CloudWatch alarm evaluation timing, SNS delivery and Lambda cold start all vary. **The measured value will be established by a deliberate load test after Phase 1 deployment** and will replace this estimate. See the Phase 1 Definition of Done.

### Estimated worst case before the Ad Fuse trips

```text
Traffic in the expected response window, bounded by the 20 rps route throttle
  20 rps x 180s                                            =  3,600 requests
  + the triggering minute (up to 1,200)                    = ~4,800 dynamic requests

Estimated cost of one trip event
  API Gateway  4,800 x $1.00/M                             =  $0.005
  Lambda       4,800 x $0.20/M + duration                  =  $0.002
  CloudFront   ~14,400 requests (or plan allowance)        =  $0.014
  DynamoDB / Firehose  rejected before write               =  $0.000
                                                              --------
                              estimated cost per trip      =  ~$0.02
```

**Estimated**, because it assumes the throttle holds, the alarm fires on the first breaching datapoint, and the fuse Lambda succeeds on the first attempt. The load test will produce a measured figure for each of those.

### Recovery procedure

```text
1. SNS email arrives naming the fuse, the route group, the metric value, and the timestamp
2. Inspect:  adlabctl fuse status        -> state per fuse, trip reason, trip time
             Athena / CloudWatch          -> source IPs, routes, user agents
3. Remediate: WAF IP or geo block, tighter route throttle, or fix the legitimate cause
4. Re-enable: adlabctl fuse reset --ad | --analytics
              restores concurrency, disables the block rule, writes an audit record
```

**No automatic re-enable and no time-based recovery.** A self-resetting fuse lets an attacker run a duty cycle.

### What the fuse does and does not guarantee

**It is expected to:** bound origin-service spend to near-zero within the expected response window; keep the static games site online throughout; require a human decision to restore service; live entirely in Terraform; affect only this project's resources. Every one of those is an expectation until the post-deployment load test measures it.

**It does not:** eliminate CloudFront request charges accrued before it trips (bounded contractually on a flat-rate plan, not on pay-as-you-go); protect against a distributed attack that stays under every per-IP WAF limit while remaining below the fast threshold — that is what the daily fuse is for; or survive its own failure, which is why there are four independent tiers plus a manual daily check during active development.

**I will not claim a hard monetary ceiling, because AWS does not provide one for most of this stack.** Every figure in [cost-model.md](cost-model.md) is an **estimated** exposure with its assumptions stated. After Phase 1 we deliberately stress-test the fuse and replace the estimates with **measured** numbers.

---

## The Publisher: Mini-Games Site

Product rationale, ad-placement policy, event model and Publisher metrics are in [publisher-product.md](publisher-product.md). The technical shape:

```text
apps/publisher/
  index.html                    game room home            ->  S3 / CloudFront
  xo/index.html                 Game #1                   ->  S3 / CloudFront
  connect-four/index.html       Game #2 (if free)         ->  S3 / CloudFront
  static/
    styles.<hash>.css           shared, long max-age, immutable
    game-room.<hash>.js         game logic (XO + C4)
    adtag.<hash>.js             ad request, render, tracking
  ads.txt                       placeholder form only
```

Static, content-hashed filenames with a long `max-age` are not cosmetic — they are what makes the Model A caching assumption in the cost model true, and they are the difference between ~3.8 and ~5.8 CloudFront requests per ad request.

### How the pieces connect

```text
xo/index.html
   |  loads styles.css, game-room.js, adtag.js
   |
   +-- game-room.js
   |      owns board state, AI, rendering, sound
   |      emits product events via a small hook interface:
   |        track('game_start', {...})   track('game_end', {...})
   |        track('game_replay', {...})  track('game_switch', {...})
   |      knows nothing about advertising
   |
   +-- adtag.js
          on page load:
            POST /ad/request  { placement_id, game, session_id, device_type }
            <- { creative, tracking_token, request_id }  |  { no_ad, request_id }
          renders the creative into #ad-slot inside a sandboxed iframe
          fires GET /event/impression?token=... on render
          fires GET /event/click?token=... on click, then navigates
          collapses the slot on no_ad or on error -- the game is never blocked
```

`game-room.js` and `adtag.js` are deliberately separate and do not import each other. They communicate only through `session_id` and the shared `track()` transport. That boundary is the point: it is the same separation that exists between a real publisher's product code and the ad stack embedded in it, and it is what makes "does advertising hurt the product?" an answerable question rather than an opinion.

---

## Approval Gates Before Phase 1

1. ✅ AWS account `<account-id>` confirmed personal (profile `<aws-profile>`, always passed explicitly)
2. ✅ GitHub `litansh` confirmed personal; repo-local git identity, global work identity untouched
3. ✅ Account plan confirmed FREE — flat-rate plans unavailable, edge is pay-as-you-go
4. ✅ Domain `xoxoxo.live` registered at GoDaddy; Phase 1 creates the Route 53 hosted zone and you delegate NS
5. Approve the first Terraform plan

## Sources

- [CloudFront flat-rate pricing plans — features, allowances, quotas](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/flat-rate-pricing-plan.html) · [Plan tiers and account requirements](https://docs.aws.amazon.com/PricingPlanManager/latest/UserGuide/plans.html) · [Launch announcement (Nov 2025)](https://aws.amazon.com/about-aws/whats-new/2025/11/aws-flat-rate-pricing-plans)
- [Restrict access to a Lambda function URL origin (OAC, payload hash requirement)](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/private-content-restricting-access-to-lambda.html)
- [Throttle requests to HTTP APIs in API Gateway](https://docs.aws.amazon.com/apigateway/latest/developerguide/http-api-throttling.html)
- [Google Ad Manager — dynamic allocation](https://support.google.com/admanager/answer/3721872)
