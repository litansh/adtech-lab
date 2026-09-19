# Learning progress

What each phase actually taught, with the reasoning worked through rather than
asserted. Written to be re-read.

---

# Phase 1 — the ad server

## The five questions, answered

These are grounded in numbers this system actually produced, not textbook cases.

---

### 1. Why the budget landed at `$5.004`, not `$5.00`

**What happened.** A $5.00 daily budget at a $4.00 CPM stopped at **$5.004** —
four tenths of a cent over.

**Why.** Eligibility asks *"has this line item already spent its budget?"* at
**decision** time. The charge happens at **impression** time. So with $4.996
spent the line item is still eligible, serves once more, and charges another
$0.004. The overshoot is exactly one impression's value.

There is a second, larger source under load: the serving-spend read is cached
for ~10 seconds. Several concurrent invocations can each read the same stale
figure and each conclude there is budget left. Under real traffic the overshoot
is bounded by *concurrency × impression value*, not by one impression.

**How to make it exact.** Reserve the spend atomically at decision time — a
DynamoDB conditional write that both checks and increments, failing the
decision if it would exceed budget.

**What that costs — the obvious one.** A conditional write on *every decision*
rather than a counter update on every *impression*. More write units, and it
moves a database round-trip onto the hot path, straight into the p99.

**What that costs — the one that matters.** You would be charging at decision
time, so you would bill the advertiser for **ads that never rendered.** Our own
data says that is 8.3% of decisions. Over-delivering by four tenths of a cent is
a rounding error; systematically billing for impressions that did not happen is
the thing advertisers audit you for.

> **This is why real ad servers accept bounded over-delivery.** It is not
> sloppiness — it is choosing the cheaper error. Contracts are written with the
> tolerance built in.

---

### 2. Two gaps: 20% unfilled, 8.3% decided-but-never-rendered

**Investigate the 8.3% first** — even though the 20% is more than twice as large.

The 20% has a *known* cause: our trace shows `geo_mismatch`, a US-only line item
against Israeli traffic. That is a demand problem with an obvious lever — buy
more demand, or target better. Unpleasant, understood, priceable.

The 8.3% is money we already decided to earn and then did not. And nothing in
our logs explains it, because **the causes are all outside the ad server**:

- an ad blocker removed the slot
- the user navigated away before the creative loaded
- the slot never entered the viewport
- a JavaScript error on the page broke the tag
- the creative failed to load

**The principle:** a known loss can be priced. An unexplained loss cannot, and
it is also precisely the gap an advertiser will dispute at invoice time — the
publisher counts a decision, the advertiser counts a render. Chase the number
you cannot explain before the number you merely dislike.

---

### 3. What the publisher buys by taking $12 instead of $15

**It buys the ability to sell guarantees at all.**

A guaranteed line item is a *contract*: the publisher promised a volume of
delivery. Break it whenever a higher spot bid appears and you cannot sell
guarantees any more — and guaranteed, direct-sold inventory is typically a
publisher's highest-CPM and highest-margin business, because the advertiser is
paying a premium for certainty.

Take the $15 every time and three things follow:

1. **Under-delivery on the guarantee**, which means make-goods — free inventory
   later — or a refund. The $3 gained becomes a larger loss.
2. **The premium disappears.** Nobody pays extra for certainty you do not
   provide. You have converted a contract business into a spot market.
3. **The spot price is not stable.** $15 today is not $15 next quarter. You
   traded a predictable book for an unpredictable one.

> Priority-before-price is not the ad server being naive about money. It is the
> ad server honouring a contract that is worth more than the individual
> impression it costs.

---

### 4. The floor: $1.50 → $3.00

Revenue per 1,000 opportunities is `CPM × fill`:

```
before    $2.10 × 0.72  =  $1.512
after     $3.40 × 0.38  =  $1.292
                           ─────────
                           down ~15%
```

**Revenue fell.** The CPM headline went up 62% and the business got worse — the
single most common yield mistake, and the reason `RPM ≈ eCPM × fill` is worth
memorising.

**What you would need to know before raising it further:**

- **Can the unfilled 62% be monetised another way?** House ads, a backfill
  partner, a second demand source. If yes, a high floor costs far less than it
  appears, because rejected impressions are not wasted — they fall through.
- **What is the shape of the demand curve?** A smooth decline is very different
  from a cliff at a round number where a major buyer's own cap sits.
- **Is the lost fill uniform, or concentrated?** Losing the bottom decile of
  buyers is healthy. Losing one large buyer who happens to bid below $3.00 is
  a different problem with a different fix.
- **Over what window?** Buyers adapt to floors over days and weeks. A one-day
  measurement will mislead you in both directions.
- **Second-order:** does a higher floor change *who* bids over time? Some buyers
  read floors as a quality signal.

---

### 5. Why `realised_ecpm` matching the configured CPM exactly is suspicious

Ours matched at exactly `$4.00` because the system is tautological right now: we
charge `rate ÷ 1000` per impression and then compute `spend ÷ impressions ×
1000`. Of course it matches. Nothing has yet intervened between the two.

In production, things intervene:

**Downward — non-billable impressions.** Buy on viewable CPM and only ~60-70% of
served impressions qualify. Filter IVT and more drop out. The publisher serves
1,000 and bills for 650, so realised eCPM measured against *served* impressions
falls well below the contracted rate. Reporting discrepancy does the same thing
by another route: the advertiser's ad server counts 2-10% fewer impressions than
the publisher's, and pays on *their* number.

**Upward — a CPC line item outperforming.** We rank CPC demand by
`expected_ecpm = rate × expected_CTR × 1000`. If actual CTR beats the expected
figure, realised eCPM comes in *above* the number used at decision time. The
decision was made on a forecast; the revenue is settled on reality.

> When a modelled number and a measured number agree perfectly, the usual reason
> is that they are the same number wearing two hats. Find what should have
> intervened and did not.

---

## Concepts, and what to remember

| Concept | The thing worth keeping |
|---|---|
| Ad request / opportunity / impression | Three different things. Every metric is a ratio between them |
| Eligibility vs targeting | Targeting is *part of* eligibility, not a separate universal stage |
| Priority before price | A guarantee is a contract worth more than the impression it costs |
| eCPM normalisation | Without it you cannot rank mixed demand — and an auction is just this comparison across independent buyers |
| The four fill metrics | There is no universal denominator. Name which one you mean, every time |
| `RPM ≈ eCPM × fill` | Higher CPM routinely means lower revenue |
| serving state vs billing ledger | Different consistency requirements. Merging them is the most misleading simplification available |
| Bounded over-delivery | Choosing the cheaper error, deliberately |
| Measurement integrity | If any URL can mint an impression, every metric above it is fiction |
| Latency | In AdTech it is *eligibility*: past `tmax` you were never in the auction |

---

## What building it taught that no document predicted

The design docs were written before deployment. Reality corrected them five
times, and the corrections were more instructive than the plan.

### Free-plan constraints, found only by applying

| Discovery | Consequence |
|---|---|
| **Firehose unavailable** on the AWS Free plan | The designed event pipeline did not exist. Replaced with per-invocation S3 writes — fine at ~$0.23/month now, ~$10/month at 1M requests, which is where a real buffering layer becomes necessary |
| **Lambda account concurrency is 10**, and 10 must stay unreserved | *No function can reserve anything.* Reserved concurrency was never a spending cap anyway — and cost-fuse can no longer reserve capacity, so a flood could in principle starve the thing meant to stop it |
| **CloudFront flat-rate plans unavailable** | The only contractual "no overage charges" cap in the stack is off the menu. There is now **no hard monetary ceiling anywhere** |
| **`route53domains` unavailable** | Domain bought at an external registrar; Route 53 serves DNS after delegation |

### Bugs, and why each one hid

| Bug | Why the tests missed it |
|---|---|
| Double `RUnlock` killed the Lambda on its first request | Every decision test used `MemoryBudget`. The DynamoDB path had **zero coverage** |
| `attributevalue` ignores `json` tags — it uses `dynamodbav`, else Go *field names* | Items unmarshalled into empty structs, so every request returned `no_ad`. Nothing tested the seeded-data path |
| CloudFront rewrote the ad server's **403 into a 404** | A distribution-wide error response, meant for missing S3 keys, was mangling a security response. Tests hit `httptest` and the API directly — **nothing exercised the path through the CDN**, which is the only place the bug existed |

> The pattern is one lesson, not three: **every bug lived in a boundary no test
> crossed.** Between code and database, between fixture and store, between
> origin and CDN. Unit tests do not find those, and neither does a plan.

---

## Phase 1 status

| | |
|---|---|
| Live | https://xoxoxo.live |
| Cost | $0.51–2/month, covered by credits until 2027-02-08 |
| Revenue | **Simulated.** House campaigns, virtual ledgers |
| Still unverified | The Cost Fuse has never been fired **by an alarm** — only by hand. Every exposure figure in `cost-model.md` remains an estimate |

**Open for Phase 2:** pacing, frequency capping, bid floors, viewability — and
the load test that replaces the fuse estimates with measurements.

---

# Session log — 2026-08-28

## What shipped

**2048**, the fourth game. DOM tiles rather than canvas, because the entire feel
of the game is tiles *sliding* and CSS transforms give that for free with GPU
compositing. One level of undo; reaching 2048 offers to continue rather than
ending the run.

**A regression test that drives a real browser** — `make test-site`, 104
assertions across a laptop and an iPhone viewport. It uses headless Chrome over
CDP through a ~150-line stdlib websocket client, rather than adding Playwright
and a downloaded browser to a site of four static pages.

**A personal records table** on the home page: played, won, best.

**Traffic classification before the auction** (`filter.go`) and an
**experimentation layer** (`experiment.go`) where every money-affecting number
is a variant.

## The lesson of the session: adding the fourth game exposed a bug in all four

Every board was sized by width alone:

```css
width: min(92vw, 460px);
```

On a 390×664 phone that is a 359px square with roughly 340px of vertical room
left after the chrome. **Every game was 160–224px below the fold on a phone**,
and 100–220px below on a laptop. The topbar was the other half: it wrapped to
three rows and ate **157px of a 577px screen**.

The fix is to bound the board on *both* axes against a measured `--chrome`
budget, and to use `dvh` rather than `vh` — on iOS Safari `vh` ignores the
address bar, which would have reintroduced the identical bug on the device that
matters most.

> This shipped, live, for weeks. Four games, reviewed by eye more than once, and
> nobody caught it — including a product review that specifically looked at
> above-the-fold placement and measured only the **desktop** viewport.
>
> It was found in the first ninety seconds of having a test that opens a phone
> viewport. The lesson is not "we should have looked harder." It is that
> **an unmeasured requirement is not a requirement.**

## Bugs found by verifying rather than assuming

| Bug | How it hid |
|---|---|
| The test served a **stale build** and reported all-pass | `site_check` only rebuilt when `build/` was missing. A test that can pass against stale output reports the last known-good state as current. It now always rebuilds |
| `records.js` was **never linked** on the home page | My insertion anchor (`adtag.js`) exists only on game pages. `str.replace` silently does nothing when the anchor is absent, and I printed "linked" anyway. Verification caught what the script's own success message did not |
| Missing `w`/`h` classified real users as **fraud** | The placement defines the slot size, so a client omitting dimensions is normal. Treating *absent* as *impossible* marked most legitimate traffic non-billable. The existing handler tests caught it — the classifier was wrong, not the tests |
| The nav check compared `textContent` | After wrapping labels in `.lbl`, tab text became `"Tic-Tac-ToeXO"`. The contract in `games.json` is what made this a one-line fix rather than a hunt |

## Design decisions recorded

**[ADR 0006](adr/0006-every-money-number-is-a-variant.md) — every number that
affects money is a variant.** The Phase 3 floor sweep was run offline, once, by
hand. Every number it produced is already stale, because buyer behaviour is not
a constant either. So: no constant, ever. Deterministic bucket assignment,
a mandatory ≥5% holdout, all learning in the cold path.

The subtle part is **buckets rather than live weights**. Assigning from weights
reshuffles every user on every controller update, which destroys session
metrics and makes results irreproducible. With fixed bucket ranges, shifting 10%
of traffic moves only the sessions at the boundary — asserted directly by
`TestWeightShiftMovesOnlyBoundaryTraffic`.

**[traffic-filtering.md](traffic-filtering.md) — filtering is four layers, not
one.** Edge/WAF blocks for **cost**. Product decides who we **want** (crawlers:
yes, enthusiastically — assistant crawlers are how people increasingly find
anything). Business decides who gets **charged**. Cold path **revokes** what the
first three got wrong.

The line that matters is **declaration, not automation**. A crawler that
identifies itself is a good citizen; a headless browser spoofing an iPhone from
a datacentre is not. The difference is honesty, not technology. And declaration
must beat suspicion — if honesty scores worse than silence, nobody declares
themselves twice.

We also **serve** suspected IVT a house ad rather than blocking it. A block is a
free oracle telling the operator which signals we detect; a house ad tells them
nothing and costs nothing, because there was no revenue in it either way.

**[agentic-readiness.md](agentic-readiness.md)** — what the agentic era actually
requires, and the structural reasons most SSPs are not ready. The short version:
an SSP that reclassifies traffic as non-billable reduces its own revenue this
quarter, and traffic quality reports to a different function than yield. That is
an org-design problem, not an engineering one, and it explains more than the
other six reasons combined.

## Numbers measured this session

| | Before | After |
|---|---|---|
| Board below the fold (iPhone 390×664) | 160–224px | **0 — all four fit** |
| Board below the fold (laptop 1280×676) | 100–220px | **0** |
| Mobile topbar height | 157px | ~50px |
| Automated assertions | 0 | **104** |
| Ad-server tests | 21 | **40** |

## Still open

- **Layer 4 has no job.** Events carry `traffic_class`, `billable` and
  `traffic_reasons`; nothing yet revokes billability retroactively. This is the
  honest gap in the filtering design
- The cold-path controller (Thompson sampling) is **specified, not built** —
  the experiments are seeded and serving, but nothing moves the buckets yet
- The Cost Fuse has still never been fired **by an alarm**
- Precision and recall of the traffic classifier are **unmeasured**. There is no
  ground truth in this dataset, so the thresholds are thresholds, not accuracies

---

# Session log — 2026-08-29

Track A finished. Phases 3, 4 and 5 in one stretch, plus four of the seven
agents. What follows is what was learned, not what was built — the build is in
the git history.

## Phase 3 — yield mechanics

**Frequency capping is the one place privacy and effectiveness genuinely
collide.** Most privacy conflicts in AdTech dissolve on inspection: you did not
need the identifier, you needed the aggregate. This one does not dissolve,
because a cap is *defined over a person*. We built the honest version —
session-scoped — and wrote down exactly what it cannot do: no capping across
sessions or devices, no measurement of true frequency, and no resistance to a
client that discards its session id. That last one is acceptable while the
advertiser is us and stops being acceptable the moment someone pays.

**Pacing's design is in how you slow down, not in the arithmetic.** A hard stop
produces sawtooth delivery, so the throttle is probabilistic. A throttled line
item never drops below 5%, because a line item at zero has no observations and
cannot notice that conditions changed. And it never *stops* — budget exhaustion
stays a separate check, since conflating them makes a delivery problem and a
budget problem report identically.

**The interaction nobody checks:** a frequency cap and a pacer are each
reasonable and together make a budget undeliverable. Measured: 20 sessions at a
cap of 3 spend $0.24 of a $5.00 budget, projecting a 90% miss.

> A cap constrains opportunities per person. A budget assumes enough people.
> Nothing checks that those two assumptions agree.

**Viewability produced the most uncomfortable finding of the phase.** The ad
slot was 0% visible on load on every game at every viewport, needing 166–289px
of scrolling. That is the direct consequence of a deliberate product decision —
the board goes above the fold, so the ad goes below it — and we did not reverse
it. Instead: stop requesting ads nobody will see. Cost, viewability rate and
buyer fairness all improve, and the player is unaffected.

## Phase 4 — a buyer across a real network boundary

The buyer became a separate process with its own campaigns, budget and clock.
Four things stopped being simulations: `tmax` as an enforced deadline, learning
you won from a notice that can be lost, failures as ordinary no-bids, and
parallel fan-out against one shared deadline.

**Then the numbers stopped agreeing, which was the point.** 300 requests: the
seller counts 251 wins and $2.4180; the buyer counts 203 and $1.9516. A 19.1%
gap, neither side wrong. The dominant cause is lost win notices — everything the
buyer believes about delivery arrives through one fire-and-forget GET.

The industry does not fix this. It bounds it: a contractual tolerance, sell-side
numbers as the billing record, and a shared event id so the gap is
*decomposable* rather than merely visible.

**Three bugs appeared that could not exist while the buyer was a loop:** a
relative `nurl` the seller cannot resolve (100% discrepancy, no error); a
network buyer's creative looked up in the seller's own control plane, turning
every win into a blank slot; and buyer latency assigned in a `defer` on an
unnamed return value, so every buyer reported 0ms.

## Phase 5 — supply chain

`ads.txt`, `sellers.json` and `schain` only mean something together, and the
join runs one way: schain node → the `asi` domain's `sellers.json` → the
seller's domain → *that* domain's `ads.txt`. Any one alone proves nothing.

**Writing the verifier caught our own bug immediately:** the schain carried the
*site* id where the *seller* id belongs. Well-formed, and it fails verification.

## The agents

Four of seven built. The order is deliberately the arbitration order in
reverse — brakes before accelerator.

**Yield** proposes, never applies. **Integrity** revokes billability
retroactively using evidence that did not exist at serve time. **FinOps**
attributes cost by measured activity rather than by revenue share, because
revenue-proportional attribution is circular and *cannot* discover an
unprofitable publisher. **Demand** decides who is worth calling.

**Shadow mode** is the stage that makes any of this trustworthy. The agent
computes what it would have done, applies nothing, and the counterfactual is
computable because per-arm rates were observed. `Verdict` refuses promotion on
three grounds, and the third matters most: a good average with one catastrophic
day. An agent that gains 3% by winning small and losing big is a different
proposition from one that gains 3% consistently.

## The lesson that repeated most

Six separate bugs this session were **something reporting success for work that
did not happen**:

- a test serving a stale build and reporting all-pass
- `records.js` never linked, because `str.replace` silently no-ops
- the nightly workflow patch that did not match and printed success anyway
- an ad slot that could never fill, because `display:none` means the observer
  never fires
- a fold assertion satisfied by a path that never reached the thing being tested
- buyer latency discarded by a `defer` on an unnamed return

> The common shape: **absence of an error is not evidence of success.** Every one
> was found by checking the *result* rather than trusting the operation, and
> every one had been silently wrong for a while.

## Also

Layout budgets were tuned to one machine three times before the cause was found:
**a requested window height maps to a different viewport height on every
platform.** Fixed by forcing the viewport over CDP, which ended the whole class.

And an ads.txt-adjacent finding worth keeping: our S3 lifecycle expired raw
events at 365 days while `privacy-baseline.md` promised 90. Four times the
commitment, unnoticed, because the document is the thing people read. **A policy
nobody verifies is a belief.**
