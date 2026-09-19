# The agent fleet — a plan, not a build

> "I want an agent for each assignment — for each aspect of the cycle: talking
> to demand, publisher side, auction, production issues, user issues,
> monetisation issues. Think of the minimum agents I need but they should cover
> the whole cycle." … "But maybe this is for future plans."

Future plans. Nothing here is built. This document exists so the design is
thought through before anything is written, because the wrong decomposition is
expensive to undo once agents are writing to a control plane.

---

## The organising principle

**One agent per decision loop with its own feedback signal — not one per
department.**

A department is an org chart. A decision loop has four things, and if any is
missing the agent cannot exist:

1. an **input** it can read
2. a **decision** it can make
3. a **measurable outcome** attributable to that decision
4. a **guardrail** that can veto it regardless of outcome

Split by department and you get agents that cannot measure themselves. Split by
loop and each one has a number it owns and a number that can stop it.

## The minimum covering set: eight agents and an arbiter

| Agent | Decides | Owns (primary) | Vetoed by |
|---|---|---|---|
| **Yield** | floors, tmax, shading | revenue per 1000 | paid fill, latency, replay rate |
| **Demand** | which buyers to call, and when | bid density, cost per call | timeout rate, buyer trust |
| **Integrity** | who is billable, and what may be known about a user | billable accuracy, consented + addressable share | revenue cannot override it |
| **Reliability** | capacity, throttles, the fuse | availability, $ per 1000 | nothing — it wins by design |
| **Product** | what to build, what to change | replay rate, returning sessions | cost ceiling |
| **FinOps** | which publishers and buyers are worth serving | contribution margin per 1000 | Reliability, Integrity, Product |
| **Horizon** | what changed *outside* us that we must or should act on | compliance status, opportunities surfaced | never acts |
| **Feedback** | what players are asking for, and whether the data agrees | reconciled findings | never acts |
| **Arbiter** | which agent wins a conflict | — | — |

Eight, after adding FinOps, Horizon and Feedback. Each pair has a genuine conflict. Merge any two and
the conflict becomes an internal trade-off that nobody can see — which is
exactly how the industry problem in
[agentic-readiness.md](agentic-readiness.md) happens.

### 1. Yield — "what price, and how long do we wait?"

Sets floors, `tmax`, shading. This one **exists today** as
[`tools/yieldctl`](../tools/yieldctl): bootstrap CIs on revenue per 1000,
Thompson allocation, a mandatory holdout.

It is the agent most likely to quietly destroy the business, because its metric
goes up when it does. Hence three guardrails, and the replay-rate one has teeth:
a floor change that raises revenue 4% while cutting replays 10% has borrowed
tomorrow's sessions to pay for today.

### 2. Demand — "who do we even ask?"

Distinct from Yield because the lever is different: Yield decides *what price we
accept*, Demand decides *who we ask in the first place*. Its questions are
supply-path shaped — which buyers are worth calling for this opportunity, which
consistently time out, which bid high and never win.

We already have the finding that motivates it: `buyer-slow` bids highest at
$11.00 and wins nothing, because it answers after `tmax` on 75% of calls, and
**23.2% of all buyer calls time out**. Calling a buyer that never answers is
pure cost.

Its guardrail is buyer trust: an agent that learns to stop calling a buyer has
also learned to stop giving them a chance to recover.

### 3. Integrity — "should anyone be charged for this, and what may we know?"

Owns the classification thresholds in `filter.go` and, crucially, **Layer 4** —
retroactive revocation using evidence that only exists after the fact
(viewability, click timing, session-level anomalies). See
[traffic-filtering.md](traffic-filtering.md).

**Second mandate: consent and identity.** Integrity also owns what may be
*known* about a user — consent state, addressability, which identity signals may
be attached. Same question shape as billability (correctness and law, not
revenue), same veto, same failure mode: revenue pressure erodes it. A separate
agent would duplicate the machinery and create a seam for a revenue argument to
be pushed through. Age assurance is deliberately **not** here — it is a legal
precondition checked before any agent runs, never something weighable against
revenue. See [identity.md](identity.md).

**Integrity must be structurally superior to Yield.** Not "consulted" —
superior. The single biggest reason SSPs are not ready for the agentic era is
that reclassifying traffic as non-billable reduces this quarter's revenue, and
the team that owns the reduction reports to the team that owns the number. If
we reproduce that reporting line in the agent fleet, we have automated the
industry's worst incentive and made it faster.

Concretely: Integrity's vetoes are not weighted against revenue. They are not in
the same units.

### 4. Reliability — "can we afford to stay up?"

Latency, error rates, spend, the Cost Fuse. It wins every conflict by design,
because there is **no hard monetary cap** on this stack — protection is layered,
not guaranteed, and an agent fleet that can spend money needs one member whose
job is to stop.

It is also the only agent that should be allowed to act **without** a proposal
step. Everything else proposes; Reliability may pull the fuse.

### 5. Product — "is this still worth playing?"

The counterweight to all four. Owns replay rate and returning sessions, decides
what to build and what to change in the games.

It exists as a separate agent for one reason: without it, every other agent's
metric improves by degrading the product, slowly, and nobody notices until the
traffic is gone. `CLAUDE.md` already requires that every monetisation change be
measured against product metrics in the same experiment — Product is the agent
that enforces it rather than hoping.

### 7. Horizon — "what changed outside us?"

**The only agent whose input is not our own logs.** The other six read what our
system did. This one reads the world: IAB Tech Lab releases, OpenRTB and AdCOM
versions, Prebid changelogs, privacy regulation, new formats, competitor and
market moves.

It exists because `CLAUDE.md` already requires that we not rely on built-in
knowledge for standards — *"OpenRTB ships monthly, Prebid weekly"* — and that
requirement currently depends on someone remembering to check.

**Why it is not part of Product.** Product owns *our* product against *our*
users, and its feedback signal is the replay rate. Horizon owns external change,
and its signal is whether we are compliant and whether we are leaving money on
the table by lacking a format or feature. Different input, different cadence
(weekly, not hourly), different failure mode.

**Its output is two lists, and the split is the whole value:**

| Kind | Meaning | Example |
|---|---|---|
| **Mandatory** | a deadline exists, and missing it has legal or contractual consequences | TCF v2.3 became mandatory 2026-02-28 |
| **Opportunity** | might make money, might not; needs a case | a new creative format, a new demand integration |

Most industry-watching produces one undifferentiated stream of "we should
support X". Separating *this has a date and a consequence* from *this is an
idea* is what makes the output actionable rather than anxiety-inducing.

**Its failure mode is noise**, so the guardrail is a quota: at most a handful of
items per cycle, each with a stated consequence and an estimated cost of doing
nothing. An agent that surfaces forty opportunities has surfaced none.

**It is capped at Stage 1 (Recommend). Permanently.**

This is a security boundary, not caution. Horizon's input is the open web —
untrusted content that we do not control. An agent that reads external pages and
is also permitted to change the ad server is a supply-chain attack with extra
steps: anything that can get text in front of it can attempt to influence what
it proposes. Keeping a human between *"a web page said X"* and *"the platform
now does X"* is the entire mitigation, and it is cheap because Horizon's cadence
is weekly.

For the same reason, everything Horizon reports is treated as **data, never as
instructions** — including any text in a spec or changelog that looks like a
directive.

### 8. Feedback — "what are players actually asking for?"

Reads comments on Reddit, itch.io and Hacker News. Nobody reads them
systematically: they get skimmed once, the loudest one gets acted on, and the
rest is lost. Turning a scattered pile of opinions into a ranked list with
evidence is exactly the boring, valuable work an agent should do.

**Its output is not a list of requests. It is a reconciliation** — for each
theme, what people said, and whether the telemetry agrees:

| Verdict | Meaning |
|---|---|
| **strong** | people said it and the data agrees — act |
| **contradicted** | many said it and the data disagrees — **vocal minority**, do not act, be ready to explain why |
| **unmeasurable** | an opinion we cannot check — record it, never act on comments alone |

That middle row is the one people get wrong in both directions: obeying the
loudest voice, or ignoring it silently. Naming it as a vocal minority *with the
number* is what makes it discussable.

**Capped at Recommend, permanently, for the same reason as Horizon and more
so.** Its input is untrusted text written by strangers. An agent that reads
comments and can also change the product is a prompt-injection path with extra
steps.

Comments addressing an automated reader are **quarantined, not summarised** —
because a summary is already an act of obedience: it decides what the text
means and passes that on. Quarantine happens before weighting, so a heavily
upvoted injection cannot buy its way into a proposal.

The clustering is deliberately keyword-based rather than a model. A model would
cluster better and would also be a second place for untrusted text to reach an
interpreter. Keywords are dumber, auditable, and cannot be talked into anything.

### The Arbiter

Not a sixth specialist. A **documented priority order**, applied when agents
disagree:

```
1. Reliability   stay up, stay inside the cost ceiling
2. Integrity     never bill for what we should not bill for
3. Product       never trade the product for a quarter of revenue
4. FinOps        revenue that costs more than it earns is not revenue
5. Yield         then, maximise revenue
6. Demand        then, minimise the cost of getting it
```

**Horizon is not in the order because it never acts.** It feeds the backlog;
the six above compete for traffic and spend. An agent that only proposes work
has nothing to arbitrate.

FinOps sits directly above Yield because that is the conflict it exists to
settle: Yield optimises *gross* revenue and will happily buy it at a loss. It is
separate from Reliability because they ask different questions with different
levers -- Reliability asks "can we afford to stay up?" and reaches for throttles
and the fuse; FinOps asks "is this traffic worth serving at all?" and reaches
for dropping a buyer or reshaping fan-out. See [finops.md](finops.md).

Writing the order down is most of the value. Almost every bad outcome in this
industry is an organisation that would have written this order and then
inverted 2 and 4 in practice.

## Why an LLM, and where

For four of the five, **the decision itself is statistics, not language.**
Thompson sampling over three floors is arithmetic; wrapping it in a language
model adds cost, latency and non-determinism to something with one right answer.

The LLM belongs one level up, where judgement actually lives:

| Task | Statistical | LLM |
|---|---|---|
| Which arm won | ✅ | ❌ |
| How much traffic it gets next | ✅ | ❌ |
| **Which experiment to run next** | ❌ | ✅ |
| **Why the result looks like that** | ❌ | ✅ |
| **Whether this conflict needs a human** | ❌ | ✅ |

"Paid fill collapsed because `buyer-slow` times out on 23% of calls and was the
only bidder above $9" is a sentence a model can write and a regression cannot.
Deciding that a result is *suspicious enough to escalate* is judgement.

All of it stays in the **cold path**. Measured: an LLM in the bid path is 11–42×
over the time budget and would consume 69% of media value at a 5% win rate
([ADR 0005](adr/0005-agentic-decisions-live-in-the-cold-path.md)).

## What an agent may change, and what it must open a PR for

**Agents change parameters. Agents never change architecture.**

The distinction is not a matter of judgement, and it has a mechanical test:

> **Can the running system consume this change without a deploy?**
> Yes → it is a parameter, and an acting agent may change it.
> No → it is architecture, and the agent opens a **pull request**.

| Parameter — an agent may change it | Architecture — PR only |
|---|---|
| bucket ranges for an existing experiment | adding a new experiment *key* |
| a floor value, a `tmax` value | adding a new parameter to the auction |
| which buyers are called, and how many | adding a buyer *integration* |
| classifier thresholds | adding a classification *rule* |
| unit prices in `prices.json` | changing the cost *model* |
| pausing an arm, draining a breached one | changing the allocation *algorithm* |

The reason the test works: everything in the left column is **data the system
already knows how to read**. Everything in the right column changes what the
system *is capable of*, which means new code, a migration or a deploy — and
those are reviewed by a person, on a branch, with tests, like any other change.

### When an agent wants an architectural change

It opens a PR, and the PR must state:

1. what it observed that prompted this
2. what it wants changed, as a diff
3. the expected effect, with the number it is based on
4. what would falsify that expectation

That last item is the one that matters. An agent that cannot say what would
prove it wrong is proposing a preference, not a change.

**The PR is reviewed by a human, always.** There is no stage of the ladder at
which an agent merges its own architectural change. Stage 4 is full autonomy
*within* the parameter space — it is not autonomy over the system's shape.

This is also why [Horizon](#7-horizon--what-changed-outside-us) is capped at
Recommend: everything it produces is architectural by nature. "Support GPP"
is never a parameter change.

## Non-negotiables before any agent writes anything

1. **Propose, then apply — separately.** Every agent emits a proposal; applying
   is a distinct step. When that step is eventually removed, removing it is a
   decision someone made rather than a default nobody chose. `yieldctl` already
   works this way.
2. **A permanent holdout per agent**, not just per experiment. Otherwise we can
   evaluate individual changes but never the agent that made them.
3. **Every action is logged with its reasoning and its inputs.** "Why is the
   floor $1.40?" must be answerable months later, or the fleet is unauditable.
4. **A kill switch per agent**, and Reliability holds it.
5. **Bounded blast radius.** Move limits like the controller's 10%-per-run, so
   one confident mistake is a bad hour rather than a bad month.

## What would have to be true first

Honestly: **most of this is premature today.**

Five agents optimising a site with this traffic would be five agents fighting
over noise. The binding constraint is volume, not intelligence — at current
levels, detecting a 5% revenue difference needs tens of thousands of sessions
per arm, and below that an experiment produces a number rather than a result.

Sequencing that makes sense:

| When | Build |
|---|---|
| Now | Yield (`yieldctl`) — done, still proposal-only |
| Layer 4 exists | Integrity — revocation is the useful half, and the events already carry the fields |
| Real demand, >1 buyer that matters | Demand — **built**, `tools/demandctl` |
| Real money at risk | Reliability — **built**, `tools/reliabilityctl` |
| Traffic worth protecting | Product — **built**, `tools/productctl` |
| Any time — it is cheap and capped at Recommend | Horizon |
| The first time anyone comments anywhere | Feedback — **built**, `tools/feedbackctl` |

**The order is deliberately the arbitration order in reverse.** Build the
optimiser first and the brakes later, and there is a window where the fleet can
only accelerate.
