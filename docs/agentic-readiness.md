# Agentic readiness

> "Make it ready for the agentic era… filter traffic and proper auction, proper
> testing, and all. No way we're doing it properly at Rise, and I want to know
> why."

This document separates three things that get mixed together under "agentic",
states what readiness actually requires for each, and then gives an honest
account of why most SSPs are not ready — including the parts that are nobody's
fault.

**A caveat that matters:** I have no visibility into Rise's stack. Everything in
§4 is a structural argument about how SSPs are built and incentivised. Treat it
as a set of **diagnostic questions to ask**, not as findings about your
employer. The questions are in §5.

---

## 1. Three different things called "agentic"

### A. Agents as traffic
Software acting for a real person browses the page: shopping assistants,
comparison agents, research agents, accessibility tools.

**What breaks:** the industry's binary. "Human = billable, non-human = fraud"
has been approximately true for twenty years. It stops being true when a
non-human request carries real human intent and a real budget.

Display advertising against an agent is close to worthless — no eye sees it —
while the *intent* may be worth more than any banner that site will ever serve.
A pipeline with one bot flag cannot say "let it in, don't bill it, count it
separately, and consider selling the intent a different way."

### B. Agents as buyers
The counterparty is an autonomous system that sets its own bids, chooses its own
supply paths, and can *reason about* whether it is being treated fairly.

**What breaks:** opacity as a business model. A human buyer who suspects a take
rate is high argues in a QBR. An agent measures clearing prices across paths,
concludes a path is expensive, and routes around it — silently, permanently, and
faster than any account manager can respond. The IAB's **Agentic RTB Framework**
is the industry's attempt to give this structure.

### C. Agents inside our own pipeline
Using models to set floors, shape traffic, choose supply paths, explain results.

**What breaks:** the latency budget, immediately. Measured for this project: an
LLM in the bid path is **11–42× over the time budget** and would consume **69%
of media value at a 5% win rate** (345% at 1%). It is not a tuning problem, it
is an order-of-magnitude problem. See
[ADR 0005](adr/0005-agentic-decisions-live-in-the-cold-path.md).

## 2. What readiness actually requires

| Requirement | Why | Here |
|---|---|---|
| Three-way traffic classification | "human/bot" cannot express the agent case | `filter.go` |
| Declaration beats suspicion | if honesty scores worse than silence, nobody declares | tested |
| Layered filtering | block-for-cost ≠ don't-bill ≠ don't-index | [traffic-filtering.md](traffic-filtering.md) |
| Retroactive billability | the best evidence arrives after the decision | fields logged, job not built |
| Everything is a variant | agents change behaviour faster than humans; a hand-tuned constant is stale on arrival | [ADR 0006](adr/0006-every-money-number-is-a-variant.md) |
| A permanent holdout | you cannot evaluate a controller you never withheld | enforced by `Validate()` |
| Explainable auctions | an agent buyer will model your pricing whether or not you explain it | Decision Trace |
| Intelligence in the cold path | the hot path has no room, at any price | ADR 0005 |

The unifying idea: **an agentic counterparty measures you continuously.** Any
part of your system that only works because the other side wasn't looking
closely will stop working.

## 3. Why "proper auction" is the hard half

An agent buyer can detect, from clearing prices alone:

- whether your first-price auction actually clears at the bid
- whether floors move in response to *their* bidding (an adaptive floor is a
  second-price auction wearing a disguise, and it is detectable)
- whether identical inventory is priced differently by path
- whether "premium" placements differ from remnant in any measurable way

None of that requires access to our code. It requires patience and logs, which
is exactly what an agent has infinite amounts of.

The defence is not obscurity — it is being able to answer. That is what the
Decision Trace is for: *why did this request produce this ad at this price?*
answered from the log, months later.

Two specific traps:

**Adaptive floors.** Raising a floor because a specific buyer bids high is
extracting surplus by observing them. A human buyer may never notice. An agent
will, will label the path adversarial, and will route around it. The
`floor_price` experiment here is assigned by **session**, not by buyer, for
exactly this reason.

**Bid shading tuned only to win.** A shading model optimised purely for win rate
becomes a model of the auction's inconsistencies. When the counterparty models
you back, that turns into an arms race that both sides lose to a direct path.

## 4. Why most SSPs are not ready

Structural reasons, roughly in order of how much they explain.

**1. The incentive is backwards.** An SSP that reclassifies traffic as
non-billable *reduces its own revenue this quarter* to protect a buyer's spend.
Traffic quality reports up through a different function than yield, and yield
carries the number. This is the single biggest reason, and no amount of
engineering fixes it — it is an org design and compensation problem.

**2. Holdouts look like waste.** A permanent 5–20% control arm is, on a
spreadsheet, revenue deliberately left on the table. Defending it requires
saying "we do not actually know our optimiser works" out loud. Most teams
optimise without a counterfactual and report the winner's absolute number, which
always looks good — because it is selected for looking good.

**3. There is no room in the hot path, and no data platform for the cold one.**
Bidders are latency-bound to single-digit milliseconds. Any real intelligence
must live in the cold path, and that needs event-level logs with assignment
attribution, cheap query, and a control plane that changes without a deploy.
Many SSPs have aggregated reporting rather than event-level attribution, which
is enough to report and not enough to learn.

**4. Per-request experiment assignment was never designed in.** Retrofitting
"every parameter is a variant" into a QPS-optimised C++ bidder with global
configuration is a rewrite of the hot path, not a feature. The systems are
excellent at what they were built for, and this was not it.

**5. Statistics are done wrong in a way that is invisible.** Revenue per 1000 is
heavy-tailed. t-tests on it produce confident findings that do not reproduce;
bootstrapping the impression rather than the assignment unit understates
variance and manufactures winners. These failures do not look like failures —
they look like results.

**6. Blocking is easier to defend than not-billing.** "We blocked 12% of traffic
as invalid" is a slide. "We served 12% of traffic and chose not to bill for it"
is a harder conversation with finance, even though it is the better answer for
discovery *and* for the buyer.

**7. Nobody is counting declared agent traffic.** Most stacks block it at the
edge, so the volume is not merely unmeasured — it is unmeasurable, because the
requests never reach anything that logs them. The leading indicator for whether
agentic monetisation matters is being deleted before it can be observed.

## 5. Questions worth asking at work

These are diagnostics, not accusations. Each has a cheap answer if the capability
exists and an expensive silence if it does not.

1. What fraction of our traffic self-declares as an agent? If we cannot answer,
   is that because it is zero, or because we block it before logging it?
2. Do we have a permanent holdout on our yield optimiser? If not, how do we know
   it is better than a constant?
3. Can we attribute revenue to an experiment arm at event level, or only in
   aggregate?
4. When we declare traffic invalid, do we block it or decline to bill it? Who
   made that choice, and was it a cost decision or a quality decision?
5. Do our floors respond to individual buyer behaviour? Could a buyer detect
   that from clearing prices alone?
6. Which team owns the number that goes down when traffic quality improves?
7. How long does changing a floor take, end to end? If the answer is a deploy,
   we cannot run experiments at the rate the question needs.

Question 6 is the one that predicts all the others.

## 6. What this project does differently, and what it costs

Honest accounting — none of this is free:

- **A 5–20% holdout** knowingly runs a worse configuration. That is the price of
  knowing it is better.
- **Three-way classification** means voluntarily serving unpaid impressions.
- **Everything-is-a-variant** costs statistical power. At this site's volume
  that is the binding constraint: a handful of arms is the honest limit, and
  twenty simultaneous experiments would detect nothing at all.
- **Cold-path intelligence** means decisions are hours stale. For floors that is
  fine. For anything needing sub-second reaction, it is not, and we would need a
  different design.

The advantage is not cleverness. It is being small enough to build the
incentives in from the start, which is precisely what an established SSP cannot
retrofit.
