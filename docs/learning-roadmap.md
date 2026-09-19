# Learning Roadmap

## Principle

**Vertical slices.** Every milestone ends in a complete behaviour that can be run and inspected — not six weeks of services before a single ad is served.

## Goal order

Stated explicitly, because it decides every trade-off below:

```
1. Learn                    primary. Knowledge is the ROI.
2. Earn real money          secondary, and real -- not hypothetical.
3. Optimise and scale it    only once 1 and 2 exist.
```

## What changed after Phase 1

Three findings from actually building and deploying it invalidate the original
plan, so the plan changes rather than the findings being ignored.

**1. There is a hard deadline: 2027-02-08.** The AWS Free plan expires then, or
when the $119.94 of credits run out. Eight phases at Phase 1's depth do not fit
in five months. Pretending otherwise would mean discovering it in January.

**2. Real monetisation was gated on trustworthy accounting -- and Phase 1
delivered exactly that.** Signed, expiring tracking tokens; idempotent events;
separate publisher and platform ledgers; queryable end to end. The original plan
put real money at Phase 6, four phases away. The gate it was waiting for is
already open, so waiting is now arbitrary.

**3. The blocker on revenue is not the ad stack. It is that nobody plays the
games.** The ad server works. Programmatic demand rejects sites with no traffic
history. This is an uncomfortable conclusion for an AdTech project and it is
still the correct one: the highest-leverage work for goal 2 is the Publisher,
not the platform.

## Two tracks, run in parallel

The original single sequence conflated two different kinds of work serving two
different goals. They are separated now.

### Track A -- AdTech depth (serves goal 1, stays primary)

| Phase | What we build | The concept it buys |
|---|---|---|
| **1** DONE | Publisher + ad server + Decision Trace + analytics | Eligibility, priority-before-price, eCPM, measurement integrity |
| **2** | **The Lab**: synthetic traffic, competing buyers, first-price auction | Bids, clearing price, no-bids, timeouts, win rate, bid density |
| **3** | Yield mechanics: floors, pacing, frequency cap, viewability -- **and the cold-path policy layer** that sets them | The floor/fill trade-off, delivery control, and why intelligence belongs outside the timed path ([ADR 0005](adr/0005-agentic-decisions-live-in-the-cold-path.md)) |
| **4** | Mini DSP across a real network boundary | Buyer-side pacing, `tmax`, win/loss, reporting discrepancy |
| **5** | OpenRTB 2.6 subset, sellers.json, schain | The real protocol, supply-chain transparency |

> **Phases 2 and 3 are swapped from the original plan.** Reviewing the live
> system before starting showed that every yield concept depends on something
> the auction provides. With one eligible buyer per request, a bid floor has
> nothing to filter and fill rate is binary rather than price-responsive --
> so floors, pacing and viewability would have been machinery we could not
> observe working. See [ADR 0003](adr/0003-the-lab-comes-before-yield-mechanics.md).

### Track B -- Publisher and revenue (serves goal 2, unblocks earning)

| Step | What we build | Why it matters |
|---|---|---|
| **B1** | **More games.** Beyond Tic-Tac-Toe and Connect Four: 2048, Memory, Snake, Sudoku -- simple, replayable, no accounts | The blocker on revenue is not the ad stack. It is that nobody plays. Every other step in this track depends on this one |
| **B2** | Retention: best scores held locally, a daily puzzle, a streak -- **and a PWA**: installable to the home screen, playable offline | `returning_users` cannot be measured without an identifier, but retention can still be *designed* for. A PWA costs **$0** and buys home-screen presence, which is the cheapest retention lever available |
| **B3** | Real monetisation: affiliate or CPA first | The accounting gate is already passed. Our ad server stays in the serving path, so the learning survives contact with real money |
| **B4** | Honest traffic | Only with `TAC < revenue`, on policy-compliant traffic. Never bought to manufacture numbers |
| **B5** | Programmatic demand | Requires traffic history. Cannot be first, however much we want it to be |
| **B6** | **Native mobile app** -- gated, see [ADR 0004](adr/0004-mobile-pwa-first-native-app-later.md) | The gateway to a genuinely different domain: `app-ads.txt`, mediation vs in-app bidding, ATT, SKAdNetwork, MMPs, and **rewarded video at $10-30 eCPM against $1-5 for web display**. Gated on a real audience existing first -- building it before anyone plays repeats the ADR 0003 mistake |

**B1 is the highest-leverage work in the entire project for goal 2**, and it is
not AdTech work. Two games is a demo; five or six replayable ones is a reason to
visit. Each game is also a new `game` dimension in targeting and reporting, so
it feeds Track A for free -- more inventory variety to run auctions against.

Every game must clear the Product Quality Rule in
[publisher-product.md](publisher-product.md): worth playing with the ads removed.

### Deferred, honestly

Phases 7 (external Publisher in Shadow Mode) and 8 (Prebid, video, identity,
deals, IVT) almost certainly do not fit before 2027-02-08. They are not
cancelled; they are behind an account-plan decision that has to be made by
**December 2026** at the latest. Listing them as "planned" for this window would
be a schedule nobody believes.

### Expected AWS cost

Track A stays inside $2-10/month through Phase 5. Track B adds nothing until
paid traffic, which is its own approval gate. The $100 ceiling is not the
binding constraint -- the February date is.

## The First 12 Concepts — In This Order

Each rests on the previous one. Each has a "what happens if" test rather than a definition test.

### 1. Ad Request -> Ad Opportunity -> Impression
The unit of goods, and the three places it can be counted.
**Test:** *Where do 1M ad requests turn into fewer than 1M impressions, and who loses money at each step?*

### 2. Campaign / Line Item / Creative
Campaign is business intent; line item is the delivery contract (rate, flight, targeting, priority); creative is the asset.
**Test:** *Why is the line item, not the campaign, the object that carries targeting and priority?*

### 3. Eligibility (including targeting)
Whether a line item can serve this opportunity at all: status, flight, budget, a creative that fits, targeting predicates, frequency state.
**Test:** *Which eligibility check is cheapest to evaluate, and does evaluation order change the result or only the cost?*

### 4. Delivery decisioning: priority, delivery need, value, opportunity cost
Why price alone does not decide. Guaranteed demand, pacing pressure, and contractual obligation sit above raw expected eCPM.
**Test:** *A guaranteed sponsorship line item at $12.00 is behind schedule. A non-guaranteed line item offers an expected eCPM of $15.00. What should serve, and what does the publisher lose either way?*

### 5. CPM, CPC, CPA -> expected eCPM
Normalising pricing models to a comparable figure. `expected_eCPM(CPC) = CPC x expected_CTR x 1000`.
**Test:** *A CPC line item at $0.40 with an expected 0.8% CTR. What is its expected eCPM, and what happens to delivery if the real CTR turns out to be 0.3%?*

### 6. The four fill metrics
`request_fill_rate`, `opportunity_fill_rate`, `render_rate`, `viewability_rate` — and why platforms disagree about "fill rate".
**Test:** *A floor rises from $1.50 to $3.00. Average price goes $2.10 -> $3.40; request fill goes 72% -> 38%. What happened to revenue per opportunity?*

### 7. Budget and pacing
Budget is a constraint; pacing is a control loop.
**Test:** *A $3,000 campaign over 30 days has spent $1,400 by day 10. What should pacing do, and what is the risk of correcting too aggressively?*

### 8. Frequency capping and the identifier problem
Per-user state, its cost, and its dependence on an identifier that may not exist.
**Test:** *Why can a "3 per day" cap be exceeded even when the system is working correctly?*

### 9. Bid floors
A minimum price as both filter and signal. Hard versus soft floors.
**Test:** *What did soft floors let a sell-side intermediary capture, and why did that matter more under second-price than under first-price?*

### 10. First-price auctions, clearing price, and bid shading
The winner pays its bid; surplus capture moves to the buyer's bidding strategy.
**Test:** *A DSP's win rate moves from 4% to 28% after a shading change. What else must you look at before calling that good or bad?*

### 11. Take rate, gross media handled, and platform fee
`platform_fee = media_amount x take_rate`, and why gross media handled is not the platform's revenue.
**Test:** *A platform passes $500K to publishers on a 15% take rate and spends $4K on infrastructure. What is the platform fee, and what is its contribution profit? Separately — what is the publisher's contribution profit, and why can you not answer that from these numbers alone?*

### 12. Latency and `tmax`
`tmax` is eligibility, not performance tuning.
**Test:** *A buyer returns an excellent bid at 350ms when `tmax` was 300ms. What did it spend, what did it earn, and what does it cost the seller?*

---

## Learning Validation

After each milestone: 2-3 questions of the form above. Never "define X."

Tracked in `docs/learning-progress.md`, created in Phase 1.

---

## What Phase 1 Does Not Build

| Not building | Why |
|---|---|
| Any auction between independent buyers | Phase 3. With one demand source there is nothing to auction |
| Bidding vocabulary in the code | Phase 3. See the terminology table in `architecture-proposal.md` |
| DSP | Phase 4 |
| OpenRTB | Phase 5. A simple internal contract is enough to learn decisioning |
| Header bidding / Prebid | Phase 8 |
| Persistent advertising identifier | Gated decision. See `privacy-baseline.md` |
| Production frequency capping | Requires an identifier. Lab-only in Phase 2 |
| CMP / TCF / GPP | Phase 5, or earlier if monetisation requires it |
| Admin UI | Phase 2. A seed CLI is enough |
| Video / VAST | Phase 8 |
| Forecasting | Phase 2, naive only |
| ML bid prediction | Possibly never. Historical CTR teaches the same lesson |
| Multi-region, Kubernetes, Kafka | Not in this project |

## Definition of Done for Every Milestone

```text
1. What works                        7. Tests (unit + integration + E2E + failure)
2. One real example request           8. AWS resources created
3. What happened inside (trace)       9. Updated cost estimate and exposure ceiling
4. The industry name for each step   10. What we simplified, explicitly
5. What money does at each step      11. What I should have learned
6. Metrics                           12. The next milestone
```

---

## Phase 1 — Exact Definition of Done

Phase 1 is complete when **all** of the following are true.

### Product

1. `adlab.example/xo` serves a playable Tic-Tac-Toe game to a real browser over HTTPS, on desktop and mobile, with a correct viewport, `lang`/`dir`, and a valid HTML document.
2. The game is genuinely good with advertising disabled — verified by loading the page with the ad endpoint blocked.
3. A short privacy notice is published and accurate.
4. `ads.txt` is served in the specification's placeholder form. **No fabricated `DIRECT` or `RESELLER` entries.**

### Ad serving

5. `POST /ad/request` returns either a creative or an explicit `no_ad`, both carrying a `request_id`, with p95 server latency measured and recorded.
6. At least **three** House line items are configured via `adlabctl`, of which at least one is rejected by targeting and one is a house fallback — so the trace has something to show.
7. Eligibility evaluates: status, flight dates, budget remaining, creative size match, geo, device type.
8. Delivery decisioning applies `priority` before `expected_ecpm`, and the reason is recorded.
9. Creative renders in a sandboxed iframe; the slot collapses cleanly on `no_ad` or error, never blocking the game.

### Traceability

10. **Decision Trace** is available in the Lab for any request, showing every candidate, every rejection reason, the selection rule and the latency. It is **not** exposed in production; production returns `request_id` only.
11. Any `request_id` can be traced end to end — request, decision, impression, click — with a single Athena query.

### Measurement integrity

12. Impression and click events require a valid, unexpired, signed token. A hand-crafted URL cannot mint a valid event — verified by an explicit negative test.
13. Events carry a unique `event_id` and are idempotent on replay.
14. The click endpoint resolves its destination from the server-side creative record. It is not an open redirect — verified by an explicit negative test.

### Publisher analytics

15. `session_start`, `game_view`, `game_start`, `game_end`, `game_replay` are emitted and land in S3.
16. `session_id` is ephemeral and tab-scoped. **No persistent advertising identifier exists anywhere in production.**
17. One Athena query reports: sessions, games_started, games_completed, games_per_session, replay_rate, ad_opportunities_per_session, impressions_per_session.

### Economics

18. Publisher and platform ledgers are separate. `publisher_contribution_profit` and `platform_contribution_profit` are computed independently, on virtual money.

### Cost safety

19. The **Cost Fuse** is deployed in Terraform, is **route-aware** (separate alarms and fuses for `/ad/request`, `/event/*`, and `/collect`), and its trip has been tested end to end in the Lab: alarm fires, ad endpoints block, **the games stay online**, alert arrives, `adlabctl fuse reset` restores service.
20. A deliberate, bounded **load test against our own endpoints** has been run, and the estimated figures in `cost-model.md` replaced with **measured** ones: actual fuse response time, requests that got through before the block took effect, actual billed cost of one trip event, and confirmation that the route throttle held.
21. Verified that flooding `/collect` trips the **Analytics Fuse only** and leaves ad serving running.
22. AWS Budgets, Cost Anomaly Detection, per-route API Gateway throttling and Lambda reserved concurrency are all in place and verified.
23. Actual measured monthly cost is recorded against the estimate.

### Engineering

24. Business logic tests: eligibility, targeting, budget, selection. Integration test through the real HTTP path. One end-to-end scenario in a real browser. One failure scenario (ad server unavailable → game unaffected).
25. All infrastructure in Terraform. No manual console configuration except documented bootstrap steps.
26. Merged via PR. No secrets in the repository.

### Learning

27. I can play XO, observe the whole path through the Decision Trace, explain each step by its industry name, and answer the Phase 1 validation questions.

**Explicitly out of scope for Phase 1:** any auction, any bidding vocabulary in the code, DSP, SSP, OpenRTB, header bidding, external monetisation, persistent identifiers, frequency capping in production, consent frameworks, dashboards, admin UI, accounts, multiplayer, leaderboards.
