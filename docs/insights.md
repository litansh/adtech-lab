# Insights log

Every non-obvious finding this project has produced, with the evidence and where
it came from. Newest section first within each theme.

Two rules for this file:

- **A claim without a number is not an insight**, it is an opinion. Where a
  finding has no measurement yet, it says so.
- **Nothing about Rise goes in here.** Findings from work-scope access are
  reported in conversation only — the employer's schemas, volumes and costs are
  confidential and this is a personal repository. See the work-scope rule in
  `CLAUDE.md`.

---

## 1. Measurement — how to avoid confidently believing the wrong thing

This is the theme with the most transferable value. Almost every entry is a way
a plausible metric lied.

### 1.1 An aggregate metric hides a concentrated effect

**Evidence.** A buyer in the Lab loses 55% of its bid rate when `user.eids` is
omitted. The experiment measured:

| | bid rate |
|---|---|
| aggregate, send arm | 53.2% |
| aggregate, omit arm | 50.2% |
| **aggregate effect** | **5.7% — reads as noise** |
| **buyer-premium, per buyer** | **53.4% drop** |
| other three buyers | ~0% |

One buyer of five depends on the field; the other four dilute it **ninefold**.

**The trap.** A product reporting the aggregate concludes the field does not
matter, drops it, loses half the bid rate of its best buyer, and sees a metric
movement indistinguishable from variance.

> **The aggregate is not a weaker version of the answer. It is the wrong
> answer.**

Generalises to any experiment whose effect is concentrated in a minority of a
population.

### 1.2 Total fill was 100% at every floor, including the one that destroyed 91% of revenue

**Evidence.** The floor sweep:

| Floor | Paid fill | Revenue/1k | Total fill |
|---|---|---|---|
| $0.50 | 100% | $5.69 | 100% |
| $2.00 | 61.4% | $4.88 | 100% |
| $11.00 | 4.5% | **$0.51 (−91%)** | **100%** |

The house fallback always fills, so total fill carries no information at all.
**Paid fill is the metric with signal in it.** Same shape as 1.1 — a
population-level number concealing what happened inside it.

### 1.3 Revenue per 1000 is heavy-tailed, so the obvious statistics lie

Most requests earn nothing; a few earn a lot. Consequences:

- **A t-test reports significance that does not reproduce.** Use a bootstrap —
  no distributional assumption.
- **Resample the assignment unit, not the row.** Bootstrapping impressions when
  the experiment is session-assigned understates variance and manufactures
  winners. This is the single easiest way to declare a false positive.
- **Fix the horizon in advance.** Peeking and stopping when it looks significant
  inflates the false-positive rate badly.

Encoded in `tools/yieldctl` — the `Observation` type is deliberately per-unit so
this is hard to get wrong.

### 1.4 Attributing cost by revenue cannot discover an unprofitable publisher

Not "is unlikely to" — **cannot**. A publisher earning 2% of revenue is assigned
2% of cost, so its margin equals the platform average *by construction*. The
model will report healthy margins forever, including on the day the business
stops working.

The fix is activity-based costing from measured drivers (`tools/finopsctl`).

### 1.5 An unmeasured requirement is not a requirement

**Evidence.** "The board must be above the fold" was a stated rule. Every one of
four games was **160–224px below the fold on a phone**, live, for weeks — and it
survived a product review that specifically checked above-the-fold placement and
measured only the desktop viewport.

It was found in the **first ninety seconds** of having a test that opens a phone
viewport.

### 1.6 A test that can pass against stale output reports last-known-good as current

`site_check.py` rebuilt only when `build/` was missing, so a run could report
104 passing assertions about code that was not the code on disk. It happened
once, silently. It now always rebuilds.

### 1.7 Detectability, not blast radius, determines what can be automated

Rollback only helps for failures you notice. Much AdTech damage is slow and
quiet — a field change costing 3% of bid rate looks like variance for days.

| Class | Detection | Autonomy |
|---|---|---|
| fast + loud (errors, spend) | seconds | full, auto-rollback |
| fast + quiet (bid rate, fill) | minutes, needs a monitor built first | only once it exists |
| **slow + quiet (CPM drift, retention, buyer relationships)** | days–weeks | **never autonomous** |
| not measurable (reputation, contracts) | never | never automated |

Row three is where most AdTech value *and* damage live, and it is the row an
enthusiastic autonomy story omits.

### 1.9 A test can pass while exercising nothing

The frequency-cap fallthrough test asserted "after the caps are exhausted, the
house ad serves". It passed on the first run — and the log showed the house ad
serving from request **one**.

The request used `device_type: mobile`; the paying line item targets desktop. It
was rejected for `device_mismatch` before any cap was consulted, so the test
verified that an ineligible line item does not serve, which was never in doubt.

The fix was to assert the **whole shape**, not the endpoint: the paying line
item serves first, exactly three times, and only then does the house ad take
over. All three clauses are needed, and the middle one is what actually tests
the cap.

> An assertion about the final state can be satisfied by a path that never
> passes through the thing you meant to test. Assert the sequence.

Same family as the stale-build bug in 1.6: both reported success for work that
did not happen.

---

## 2. AdTech economics

### 2.1 Fan-out to demand costs more than compute

**Evidence.** Cost per 1000 ad requests at 3 buyer calls each:

| driver | per 1k | share |
|---|---|---|
| **buyer calls** | $0.002040 | **37.8%** |
| cloudfront | $0.001276 | 23.7% |
| api gateway | $0.001000 | 18.5% |
| dynamodb | $0.000750 | 13.9% |
| lambda | $0.000300 | 5.6% |
| **total** | **$0.005392** | |

Lambda — the thing everyone assumes is the cost — is 5.6%. **Buyer calls are
seven times larger.**

Consequence: traffic shaping is a **FinOps decision** as much as a latency one,
and FinOps and the Demand agent share a lever. Calling fewer, better buyers is
simultaneously a cost, latency and quality win.

### 2.2 A buyer that never wins is invisible to every revenue-based view

`buyer-slow` bids the highest at $11.00, times out on **75%** of calls, and wins
nothing. It generates no revenue to attribute cost against, so revenue-based
reporting cannot see it. Under cost-per-call it is the most expensive thing in
the auction.

Marketplace-wide, **23.2% of all buyer calls time out.** A timing-out buyer is
not a cheap failure — it holds the handler for the full `tmax` and returns
nothing, which is the most expensive possible outcome.

### 2.3 Infrastructure is 0.095% of revenue at healthy fill

$0.0054 per 1000 requests against $5.69 lab revenue per 1000. **The cost problem
only bites with terrible fill or huge fan-out** — which is exactly what the
demand-side view exists to surface. At healthy fill, arguing about infra cost is
arguing about a rounding error.

### 2.4 An LLM cannot be in the bid path, by two orders of magnitude

| | Hot path (LLM) | Cold path |
|---|---|---|
| Latency | **11–42× over budget** | zero added |
| Cost | **69% of media value** at 5% win rate (345% at 1%) | $0.00001/impression |
| Infra | 247× | **400× cheaper** |

Not a tuning problem — an order-of-magnitude problem. Hence
[ADR 0005](adr/0005-agentic-decisions-live-in-the-cold-path.md): the decision is
agentic, the serve is arithmetic.

And the default layer is **statistical, not an LLM**. "Which of five buyers is
worth calling" is division.

### 2.5 Programmatic display is close to the worst way to earn at small scale

Ranked by realistic return per unit of effort:

1. **Mobile rewarded video** — $10–30 eCPM vs $1–5 web display. The only path
   with a plausible route to real income, and the most gated.
2. **Affiliate / CPA** — works at low traffic, which nothing else does.
3. **Programmatic display** — what this project is *about*, and the worst
   earner at this scale.

That inversion is itself one of the more useful things the project has taught.

### 2.7 A cap and a pace are each reasonable and together starve a budget

Measured:

| | |
|---|---|
| 20 sessions, frequency cap of 3 | 60 impressions |
| spend | **$0.24** of a **$5.00** daily budget |
| projected end-of-day | $0.48 — a 90% miss |

The cap did what it was configured to do. The pacer did what it was configured
to do. Nothing in an ad server reports the collision unless something explicitly
projects delivery.

> **A cap constrains opportunities per person. A budget assumes enough people.
> Nothing checks that those two assumptions agree.**

### 2.8 Two services counting the same events disagree by 19%

Measured across a real HTTP boundary, 300 requests:

| | Seller | Buyer |
|---|---|---|
| wins | **251** | **203** |
| spend | **$2.4180** | **$1.9516** |

Neither is wrong. The dominant cause is **lost win notices**: everything the
buyer believes about delivery arrives through one fire-and-forget GET, and when
it is dropped the buyer never learns it won — permanently, silently, with no
error on either side. That single mechanism produced almost the whole gap.

The industry does not fix this. It **bounds** it: a tolerance in the contract
(commonly 5–10%), sell-side numbers as the billing record, and a shared event id
so the gap is decomposable rather than merely visible. Without the shared id you
can argue about the total and never about the cause.

### 6.x Two bugs that only appear once two processes must agree

**A relative `nurl` is silently useless.** The DSP returned `/win?bid=...`; the
seller cannot resolve a relative URL, so the call never happened. The only
symptom was a **100%** discrepancy with no error anywhere.

**A network buyer's creative is not in the seller's control plane.** The ad
server looked up the winning creative locally, found nothing, and returned
`no_ad` — turning *every* win by the real DSP into a blank slot. A buyer
supplies its own markup in `adm`. No error, no trace, and it looked exactly like
weak demand.

**A deferred assignment to an unnamed return value is discarded.** Buyer latency
was set in a `defer` on an unnamed result, so `return res` copied the struct
first and every buyer reported 0ms. A latency dashboard reading zero everywhere
looks fine.

> All three were invisible until two processes had to agree, which is the entire
> argument for building the buyer as a separate service rather than a loop.

---

## 3. Traffic quality and identity

### 3.1 The line is declaration, not automation

A crawler that identifies itself is a good citizen. A headless browser spoofing
an iPhone from a datacentre is not. The difference is **honesty, not
technology** — which is why the classifier has three outcomes, not two.

**Declaration must beat suspicion.** An agent that declares itself *and* trips
every heuristic is still classed as declared. If honesty scores worse than
silence, nobody declares themselves twice and the only signal that works
disappears.

### 3.2 Blocking is a free oracle for the attacker

A block tells the operator exactly which of their signals we detect, so they
iterate until we stop blocking. Serving a house ad tells them nothing and costs
nothing — there was no revenue in it either way.

**Blocking is a cost decision (edge/WAF). Not-billing is a business decision.**
Conflating them is expensive in both directions: crawlers you *want* get
blocked, and traffic you should never bill for gets billed.

### 3.3 Alternative IDs are downstream of authentication

UID2, EUID and ID5 are all hashed-email-plus-consent. **A publisher with no
logged-in users cannot supply any of them** — there is no email to hash.

So for most publishers, "adopt UID2" is a *login* project wearing an
integration's clothes, and "verify users" is a product problem, not an AdTech
one. The AdTech work is three days.

### 3.4 Seller-defined audiences cover 100% of traffic; ID-based cover ~5%

SDA earns less per impression and applies to everything, including the
unauthenticated majority an ID strategy can never reach. **Total uplift usually
beats the higher-CPM option covering 5% of inventory.** Chronically underused.

### 3.5 Age assurance must sit outside the agent layer entirely

It is a legal precondition, binary, not tradeable against revenue. An agent that
*could* weigh compliance against revenue will eventually weigh it wrongly.

---

## 4. Why organisations fail at this

### 4.1 The incentive is backwards, and it explains more than the engineering

An SSP that reclassifies traffic as non-billable **reduces its own revenue this
quarter** to protect a buyer's spend. Traffic quality reports through a
different function than yield, and yield carries the number.

No amount of engineering fixes this. It is org design and compensation.

**The diagnostic question that predicts all the others:**

> *Which team owns the number that goes down when traffic quality improves?*

### 4.2 Holdouts look like waste on a spreadsheet

A permanent 5–20% control arm is revenue deliberately left on the table.
Defending it requires saying *"we do not actually know our optimiser works"* out
loud. So most teams optimise without a counterfactual and report the winner's
absolute number — which always looks good, because it was selected for looking
good.

### 4.3 Nobody is counting declared agent traffic

Most stacks block it at the edge, so the volume is not merely unmeasured, it is
*unmeasurable* — the requests never reach anything that logs them. The leading
indicator for whether agentic monetisation matters is being deleted before it
can be observed.

### 4.4 Blocking is easier to defend internally than not-billing

"We blocked 12% of traffic as invalid" is a slide. "We served 12% and chose not
to bill for it" is a harder conversation with finance — even though it is the
better answer for discovery *and* for the buyer.

---

## 5. Agent and system design

### 5.1 Agents change parameters, never architecture

Mechanical test: **can the running system consume this change without a
deploy?** Yes → parameter, an acting agent may change it. No → architecture →
PR, human-reviewed, at *every* stage. Stage 4 is autonomy inside the parameter
space, never over the system's shape.

### 5.2 An agent reading the open web must never be able to act

Horizon is capped at Recommend permanently. An agent that reads untrusted pages
*and* can change the ad server is a supply-chain attack with extra steps —
anything that can get text in front of it can attempt to influence what it
proposes.

### 5.3 Never ship a decision in an SDK

Not "avoid SDKs" — the corrected rule. Be *in* the SDK for access (in-app is
where the $10–30 eCPMs are); keep **policy** server-side so agents still move in
seconds. The failure mode is an SDK that hardcodes a waterfall, client-side
floors or a compiled partner list, which welds optimisation to the publisher's
release cadence — the slowest clock in the industry.

Corollary: **put the agent where the feedback loop is fastest, and keep
everything slow-to-change out of its path.**

### 5.4 An optimiser will learn that lying pays, so some things cannot be configurable

An agent optimising bid rate learns that sending more data always helps. Every
privacy and transparency constraint looks like a bug to it.

Hence `schain`, consent, GDPR/GPP/COPPA flags are a **hard-coded refusal**, not
configuration — an experiment naming one is rejected at control-plane load, and
they have no representation in the field set at all.

### 5.5 Shadow mode is the stage everyone skips and the only one that produces evidence

`observe → recommend → shadow → bounded-act → act`. Nobody skips Observe because
it is obviously safe; everybody wants to skip Shadow because the agent already
"works". Shadow is the only stage that answers *"would this have made money, and
how often would it have been wrong?"* with data rather than assertion.

### 5.6 What ports to another company is the list of facts, not the tool

FinOps and Demand port easily. Integrity ports with a caveat. **Yield usually
does not port at all** — not because the algorithm is special, but because it
needs per-event arm assignment, and most companies have no experimentation layer
in the serving path. **The prerequisite is the assignment, not the controller.**

The most useful thing to carry into another company is the list of six facts
each agent needs, because it is short enough to check against an existing schema
in an afternoon.

---

## 6. Engineering, the hard way

Each of these cost real time.

| Finding | Detail |
|---|---|
| **Bucket ranges, not live weights** | Assigning from weights reshuffles every user on each controller update, destroying session metrics. Fixed ranges move only boundary traffic |
| **A holdout is a floor, not a pin** | The control also competes for the remainder on merit; it settles *above* the holdout, never below |
| **Floors that can be renormalised away are not floors** | Applying floors then scaling to sum to 1 pushed arms back under. A guardrail-breached arm got 32% of traffic |
| **Enforcing a constraint before a later step can redistribute past it is not enforcing it** | Movement was clamped then water-filled past the clamp: control moved 20%→46% against a 10% limit |
| **`dvh`, never `vh`, on iOS** | `vh` ignores the address bar, so a `vh`-sized board is cut off until the user scrolls |
| **Size by both axes** | A square board sized on `92vw` is 359px on a 390px phone with ~340px of room. Width alone always overflows a tall screen |
| **A budget tuned to one machine's fonts is not a budget** | The same header is 291px on macOS and 348px on Linux. CI caught it; headroom went 9px → 79px |
| **GitHub OIDC subjects now carry immutable numeric IDs** | `repo:<owner>@<ownerId>/adtech-lab@<repoId>:ref:...`, not `repo:owner/name`. Only CloudTrail showed the real claim; the workflow log said only "not authorized" |
| **A bare `build/` in `.gitignore` matches any directory of that name** | It hid the entire build toolchain. A fresh clone could not build or deploy |
| **A Go binary looks like source to `git add -A`** | It takes its directory's name and has no extension. 28MB of binaries were tracked |
| **Pointers into a slice `append` reallocates go stale** | Every click arriving after the slice grew attached to a discarded copy |
| **`attributevalue` ignores `json` tags** | It uses `dynamodbav`, else Go *field names*. Items unmarshalled to empty structs, so every request returned `no_ad` |
| **CI that lists modules stops covering new ones** | Three modules with 36 tests were silently unrun. Discover, don't enumerate |

### The pattern behind the early bugs

> **Every one of them lived in a boundary no test crossed.** Between code and
> database, between fixture and store, between origin and CDN, between macOS and
> Linux. Unit tests do not find those, and neither does a plan.

### 1.8 A stated commitment the infrastructure does not honour is worse than none

`privacy-baseline.md` promised *"raw event data retained 90 days, then the raw
records deleted."* The S3 lifecycle rule expired them at **365 days** — four
times the promise — and nobody noticed, because the *document* was the thing
people read.

Found only because writing `data-retention.md` prompted a check of what was
actually configured, against what was claimed.

> A policy nobody verifies is a belief. The gap is invisible precisely because
> the written version is the one everybody consults.

### 2.6 Cold storage classes lose on small objects

Glacier IR bills a **128KB minimum per object** against Standard's actual size:

```
standard   = $0.023 * size_kb
glacier_ir = $0.004 * max(size_kb, 128)
break-even = 0.512 / 0.023 = 22.3 KB
```

Below ~22KB, Glacier IR costs **more** than Standard, before the per-object
transition fee. Our event objects — one Lambda invocation's buffer of gzipped
NDJSON — were far under it, so the tier that exists to save money was costing
several times what it saved.

> **Storage-class optimisation is an object-size question before it is an
> access-frequency question**, and almost every "move old data to Glacier"
> recommendation omits that.

---

## 6b. The week the fleet was audited

Nine agents were built, and then someone asked whether they were actually
running. They were not. What follows all happened in three days, and every one
has the same shape.

### Nothing failed loudly, so nothing looked wrong

| What | How it looked | What was true |
|---|---|---|
| The Orchestrator's own digest | run green, `telegram: sent` absent | built the message, threw it away — the script path never resolved |
| Every Reliability alert since it was written | run green | same bug, in the agent whose only job is to page a human |
| Yield and Feedback | workflow "failing" in 0s, no log | `agent-proposals.yml` **never parsed**; they had never executed once |
| FinOps, Demand, Product, Supply chain | listed in the fleet | **no workflow ran them at all** |
| `supplychain --local` | `result: FAIL`, our chain broken | the files were fine; the tool looked in its own directory |
| The health check itself | "10 of 10 agents are not running" | a 403 for want of `actions: read`, reported as a fleet-wide outage |
| The first agent-raised issue | run green, message sent | `gh issue create --label agent` failed: **the label did not exist** |

**Five of the seven were a path relative to the repository root, in a process
whose working directory was not the repository root.** Agents run as
`go -C tools/<name> run .`. Reading code for that mistake demonstrably does not
work — I made it four times across three days, twice after documenting it. CI
now *runs* every no-traffic agent from its own directory, which costs seconds
and catches the whole class.

### An unknown is not a zero, and it is not a failure either

This log already said the first half. The health check taught the second: it
reported *"could not ask GitHub"* as *"has never run"*, and announced that every
agent was dead.

**A false alarm is the more expensive direction.** "Everything is broken" when
nothing is trains you to ignore the next one — which is precisely how four
genuinely dead agents went unnoticed. There are now three states everywhere it
matters: *could not look*, *nothing has had the chance yet*, *this is bad*.

The funnel learned the same lesson from the other end. One view across six
listings, hours old, was reported as `reach is the earliest broken stage`. True,
and useless. **Too early is not broken**, and a stage cannot be judged until
there has been time for anyone to reach it.

### `|| true` is the shell's way of swallowing an error

Every agent proposal path ended `--label agent) || true`. The create was the
*product* of the step; if it failed, the finding did not exist. All four would
have silently produced nothing, and only one had run.

The rule that came out of it: **best-effort is correct for a notification and
wrong for the artefact.** A failed Telegram message must not lose a finding
that is already on GitHub. A failed `gh issue create` must fail the run.

### Record only what actually happened

The worst one, because it hid itself. `productctl` wrote "this recommendation
was raised" *before* the workflow tried to raise it. So the run that failed to
create the issue **recorded it as created** — and the next run reported
`unchanged, nothing to raise` in a perfectly calm voice.

**The finding was suppressed permanently by the very run that lost it.** State
that means "X happened" may only be written by the step that knows X happened.

### An agent whose findings never become work is an opinion generator

The Product agent ran daily in three workflows for days: ranked answer, printed,
messaged, and then nothing. No issue, no backlog entry.

The whole argument for a fleet is that a proposal reaches a human as something
they can accept or decline. A message that scrolls away is neither. And the
split is not decoration: **an issue, not a pull request**, because product work
is architecture and architecture is where agents stop.

### Agents collide

Five workflows commit to `main`. Two overlapped for the first time and the
loser's push was rejected — so the work was done, the messages were sent, and
the commit evaporated. The repository had no record of a step the person had
just been told they finished.

It surfaced the day two of them ran together, which from then on was every day.

### The same class, outside our code

**A draft on itch.io plays perfectly for its owner** and 404s for everyone else.
A game was created, played, reported as published, and was invisible. *Playing
it yourself is not evidence that it is published* — this log's oldest lesson,
arriving through a third party's web form.

And `dist/itch/LISTINGS.md` — six games' hand-written titles, descriptions and
tags, with no generator behind them — sat inside a `.gitignore`d directory for
weeks. **The only copy of the words that sell this project lived on one laptop.**

### What actually changed as a result

Not the documentation. Seven guards that execute:

- every workflow file is parsed, and must have a `name`
- every no-traffic agent is *run* from its own directory
- `exec.Command("python3", "tools/…")` is banned
- `--label agent) || true` is banned
- a bare `git push` in a workflow is banned
- a test fails if `SaveProposal(` reappears in `productctl`
- a test fails if a listing names an asset the generators do not produce

**Absence of an error is not evidence of success** was already the first line of
this document. It was written about our own code. It turns out to apply equally
to the agents watching that code, to the workflows running those agents, and to
the third-party forms at the end of the chain.

## 6c. A distribution channel that now distrusts polish

The top comment on a game post in r/WebGames, the week these listings went live:

> "Everything about this is gen AI. Even the text is also written by AI"

More upvotes than the post itself. A second comment praising the same game was
**downvoted to −1** for describing a feature it does not have — it read as
written by someone who had not played it.

Two things follow, and the second one is uncomfortable.

**Vague praise is now a negative signal.** The downvoted comment was
complimentary. It lost points because it was generic enough to have been written
without playing, which is exactly what a generated comment looks like. Specific
beats positive, and it is not close.

**The Reddit post in `distribution-playbook.md` was written by Claude**, and
posting it as drafted invites the same top comment. Once "this is AI" leads a
thread, the thread is about that rather than about the product, and no reply
recovers it. The structure is worth keeping; the sentences are a liability.

The defence available here is unusually strong — hand-written JavaScript,
hand-authored SVG icons, covers rendered from those same icons, a seeded daily
anyone can verify produces the same puzzle for everyone. But a defence offered
before the charge convinces nobody, so it belongs in a reply and not in the post.

**The general lesson, which outlives this channel:** an audience that has learned
to detect generated content has also learned to distrust the *finish* that
generated content tends to have. Three tidy bullet points explaining elegant
design decisions used to read as care. In this channel, in 2026, they read as a
machine. That is a real change in what "good copy" means, and it applies to
anything this project publishes.

## 7. Things still unmeasured

Stated so they are not mistaken for findings:

- **Classifier precision and recall.** There is no ground truth in this dataset.
  The thresholds in `filter.go` are thresholds, not accuracies.
- **The Cost Fuse has never been fired by an alarm**, only by hand. Every
  exposure figure in `cost-model.md` remains an estimate.
- **Layer 4 revocation has never run on real traffic** — the events carry the
  fields, the job exists, nothing has consumed it.
- **No agent has run in shadow mode**, so no agent has evidence it would have
  helped.
- **Field-experiment interactions are not measured.** One field at a time; with
  *n* fields there are 2ⁿ combinations and traffic resolves a handful of arms.
