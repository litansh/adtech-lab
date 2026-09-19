# Pacing

Spending a budget **over time** rather than as fast as demand allows.

---

## Why it exists

An advertiser buying a day of a publisher's audience does not want that day
delivered between 09:00 and 09:40.

The audience at 09:00 is not the audience at 21:00 — different people, different
context, different intent. So front-loading does not buy *a day of reach*, it
buys a biased sample of one and calls it a day. Pacing is how a daily budget
becomes an actual day.

## The mechanic, and where the design actually is

```
target(t) = daily_budget × elapsed_fraction(t)
if spend > target: slow down
```

That part is arithmetic. **How** you slow down is the design.

### Probabilistic, not binary

A hard stop produces **sawtooth delivery**: spend, stop, wait, spend, stop. Each
stop is a window where the advertiser is simply absent from auctions they were
willing to win, and the pattern is visible to buyers as erratic supply.

So the throttle is probabilistic:

```
p(serve) = target / actual        (clamped)
```

Twice ahead of schedule halves the rate. Delivery stays smooth and the line item
stays present.

### The floor matters as much as the ceiling

A throttled line item never drops below **5%**. Not politeness — a line item at
zero probability has **no observations**, so it cannot notice that conditions
changed and cannot recover. Anything that stops entirely also stops learning.

### The burst allowance

Without one, `elapsed ≈ 0` at midnight, so `target ≈ 0`, so every line item is
throttled to the floor for the first minutes of every day.

That looks exactly like a bug, and it is a common one. A **2%** head start fixes
it, and the number is written down rather than tuned by feel.

### Pacing means "slow down", never "stop"

Budget exhaustion is a **separate** check and stays separate. Conflating them
makes the pacer silently enforce budgets it was never asked to enforce — and
then a delivery problem and a budget problem report the same way, which makes
both undiagnosable.

## The order of eligibility checks is a decision

```
budget exhausted   →  a hard fact
frequency capped   →  a fact about this person
paced down         →  a fact about this moment
```

Checked in that order, most-specific-true-reason first, because the Decision
Trace is only useful if the reason it gives is the *real* one. A line item
rejected for pacing when it was actually out of budget sends someone to
investigate the wrong system.

## What pacing makes possible: knowing you will miss

This is the part that is usually absent, and it is the reason to build a pacer
rather than to just not front-load.

> **Will this line item actually spend its budget today?**

`Project()` extrapolates the current rate to the end of the day. Deliberately
naive — a linear extrapolation, not a model. Traffic is not uniform across the
day, so it is wrong in a known direction, and it is still enough to answer *"is
this going to miss by a lot?"*, which is the only version anyone acts on.

It **refuses to project** below 5% of the day elapsed. A few minutes of data
extrapolated across a day produces confident nonsense in both directions, and a
projection nobody can trust is worse than no projection.

### The interaction with frequency capping

Both are individually reasonable. Together they can make a budget
undeliverable — and **nothing in an ad server says so unless something
measures it.**

Measured, from `TestCapAndPaceTogetherCanStarveABudget`:

| | |
|---|---|
| 20 sessions, cap of 3 per session | 60 impressions |
| spend | **$0.24** |
| daily budget | **$5.00** |
| projected end-of-day | $0.48 |
| on track | **no** |

The cap did exactly what it was configured to do. The pacer did exactly what it
was configured to do. The budget will miss by 90%, and the only component that
can tell you is the projection.

> A cap constrains *opportunities per person*. A budget assumes *enough people*.
> Nothing checks that those two assumptions agree.

## Configuration

| Field | Values | Default |
|---|---|---|
| `pacing` | `even`, `asap` | `even` |

`asap` is set on the house fallback: it is not buying a day of anything, it
exists to fill whatever is left, and pacing it would mean serving nothing.

## What is missing

- **Flight-level pacing.** The window is the UTC day because budgets are daily.
  A flight-level pacer would spread across the whole campaign, and would need to
  handle a flight that starts mid-day.
- **Catch-up.** A line item behind schedule is not accelerated. Bounded catch-up
  is standard and needs a limit, or a quiet morning becomes an evening spike.
- **Traffic-shaped targets.** `elapsed` is linear; traffic is not. A target
  curve shaped by observed hourly traffic would be more accurate — and is only
  worth building once there is enough traffic to observe a shape.
- **The projection is not surfaced anywhere.** It exists and is tested; nothing
  reports it yet. That is the next thing.
