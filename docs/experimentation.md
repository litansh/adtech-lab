# Experimentation

> Every number that affects money is a variant. See
> [ADR 0006](adr/0006-every-money-number-is-a-variant.md) for why.

This document is the operating manual: how an experiment is defined, how the
controller moves traffic, and — the part that most teams get wrong — how to read
the result without fooling yourself.

---

## 1. The three layers

```
  HOT PATH                    COLD PATH                  CONTROL PLANE
  (per request, <5ms)         (hourly, on logs)          (DynamoDB)

  hash(key, unit) -> bucket   read yesterday's events    experiments[]
  bucket -> variant           per-arm revenue + CIs        variants[]
  serve                       decide new bucket ranges       bucket_start
  log the assignment    --->  write control plane   --->     bucket_end
                                                             params{}
```

The hot path does no learning. It hashes and reads a range. This is
[ADR 0005](adr/0005-agentic-decisions-live-in-the-cold-path.md) applied to
yield: **the decision is agentic, the serve is arithmetic.**

The measured reason: an LLM in the bid path is 11–42× over the time budget and
would consume 69% of media value at a 5% win rate. In the cold path the same
decision costs $0.00001 per impression — about 400× cheaper — and adds zero
latency.

## 2. Defining an experiment

Experiments live in the control plane, so a change needs no deploy:

```json
{
  "key": "floor_price",
  "active": true,
  "unit": "session",
  "control_id": "ctrl",
  "variants": [
    {"id": "ctrl", "params": {"floor_cpm": "0.50"}, "bucket_start": 0,    "bucket_end": 2000},
    {"id": "f100", "params": {"floor_cpm": "1.00"}, "bucket_start": 2000, "bucket_end": 6000},
    {"id": "f200", "params": {"floor_cpm": "2.00"}, "bucket_start": 6000, "bucket_end": 10000}
  ]
}
```

Rules enforced by `Experiments.Validate()` at load time — not per request:

- bucket ranges are contiguous and cover exactly `[0, 10000)` — no gaps, no overlaps
- `control_id` names a real variant
- the control arm holds **at least 5%** of buckets (the holdout)
- no duplicate keys or variant ids

A configuration that fails validation still serves: `Assign` falls back to the
control. A bad edit must never be able to stop the ad server.

## 3. Choosing the unit

| Unit | Use for | Failure if wrong |
|---|---|---|
| `session` | anything the player experiences | a player seeing two floors in one sitting contaminates both arms |
| `request` | effects that cannot carry across requests | leaks user-level effects across arms; looks like more power, is bias |
| `placement` | effects on *buyer* behaviour | buyers learn a placement's floor over days, not per session |

Default to `session`. Justify anything else in writing.

## 4. What we measure

Never a single metric. Every experiment reports all four, and an arm wins only
if it improves the primary **without** breaching a guardrail.

| Kind | Metric | Guardrail |
|---|---|---|
| Primary | revenue per 1000 requests | — |
| Yield | paid fill, clearing price, bid density | paid fill must not fall below 40% |
| Health | timeout rate, p95 decision latency | p95 < 40ms; timeouts < 30% |
| **Product** | `game_replay` rate | must not fall — see below |

The product guardrail is not decoration. `CLAUDE.md` requires that every
monetisation change be measured against product metrics in the same experiment.
A floor change that raises revenue per 1000 by 4% while cutting replay rate by
10% has **lost** — it borrowed tomorrow's sessions to pay for today.

Total fill is deliberately absent: the house fallback always fills, so total
fill was 100% at every floor in the sweep, including the one that destroyed 91%
of revenue. **Paid fill** is the metric with information in it.

## 5. Reading the result without fooling yourself

Revenue per 1000 is **heavy-tailed**. Most requests earn nothing; a few earn a
lot. This breaks the assumptions behind the tests people reach for first.

- **Do not use a t-test on revenue.** With a long tail, one outlier session
  moves the mean and the test reports significance that will not reproduce.
- **Use a bootstrap.** Resample sessions with replacement ~10,000 times,
  compute the arm difference each time, and take the 2.5th and 97.5th
  percentiles as the interval. It makes no distributional assumption.
- **Resample the assignment unit, not the row.** Bootstrapping impressions when
  the experiment is session-assigned understates variance, because impressions
  within a session are correlated. This is the single easiest way to declare a
  false winner.
- **Fix the horizon in advance.** Peeking at a running experiment and stopping
  when it looks significant inflates the false-positive rate badly. Decide the
  duration first.
- **Correct for multiple arms.** Three arms means three comparisons; the chance
  that one looks good by luck is much higher than 5%.

### Minimum detectable effect

Power is the binding constraint on this site, not cleverness. At current
traffic, detecting a 5% revenue difference at 80% power needs on the order of
tens of thousands of sessions per arm. **Below that volume an experiment does
not produce a result, it produces a number.**

Practical consequence: run **few** experiments, with **few** arms, for
**longer**. Twenty simultaneous experiments on this traffic detect nothing at
all, however sophisticated the controller is.

## 6. The controller

Runs in the cold path, hourly. Deliberately **statistical, not an LLM** — "which
of these three floors earned more per session" is arithmetic, and an LLM would
add cost, latency and non-determinism to a division.

Algorithm: **Thompson sampling with a floor and a holdout.**

1. For each arm, model revenue per session; sample a plausible mean from its
   posterior.
2. Allocate next hour's buckets in proportion to how often each arm wins the
   sample.
3. Clamp: no arm below 5% (so a recovering arm can come back), control never
   below its holdout.
4. Move at most **10% of buckets per hour**, so a bad hour cannot swing the
   whole platform.
5. If any guardrail is breached, revert that arm to the control immediately and
   record why.

Thompson sampling rather than a fixed A/B split because it earns while it
learns; a fixed split knowingly runs the losing arm at full weight for the whole
test. The holdout is what keeps that honest.

**Where an LLM does belong:** proposing *which experiment to run next* and
explaining a result in context ("paid fill collapsed because buyer-slow times
out on 23% of calls, and it was the only bidder above $9"). Judgement about what
to try — not arithmetic about what won.

## 7. Currently running

| Key | Unit | Arms | Status |
|---|---|---|---|
| `floor_price` | session | 0.50 (holdout) / 1.00 / 2.00 | seeded |
| `buyer_tmax` | placement | 100ms (holdout) / 150ms | seeded |

`buyer_tmax` is placement-assigned on purpose: tmax changes which buyers can
answer at all, and buyer behaviour adapts over days.

## 8. Adding an experiment

1. Add a constant in `experiment.go` (a typo becomes a compile error rather than
   a permanently unassigned experiment).
2. Resolve the parameter in the handler via `exp.Assign(...)`, with the existing
   configuration value as the fallback.
3. Pass the assignment to `addExperiments` so it reaches the event log.
4. Add the arms to the control plane with a ≥5% holdout.
5. Write down, **before** starting: the primary metric, the guardrails, the
   duration, and the minimum effect worth acting on.

Step 5 is the one that gets skipped, and it is the one that makes the result
mean something.
