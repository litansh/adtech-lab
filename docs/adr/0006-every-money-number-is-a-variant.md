# ADR 0006 — Every number that affects money is a variant, never a constant

**Status:** accepted, 2026-08-28
**Supersedes nothing. Extends [ADR 0005](0005-agentic-decisions-live-in-the-cold-path.md).**

## Context

Phase 3's floor sweep produced the clearest result this project has generated:

| Floor | Paid fill | Revenue / 1k |
|---|---|---|
| $0.50 | 100% | $5.69 |
| $2.00 | 61.4% | $4.88 |
| $11.00 | 4.5% | $0.51 (−91%) |

That sweep was run **offline, once, by hand**, against synthetic buyers. Every
number it produced is already stale, because buyer behaviour is not a constant
either — a buyer that bids $9.50 today bids differently next month, and a floor
tuned to last month's demand curve quietly leaves money on the table forever.

The generalisation matters more than the floor: *every* parameter we chose by
judgement is a guess with a shelf life. The tmax that decides who is even in the
auction. How many buyers we call. Whether we return a house ad or a true no-bid.
The shading curve. Each was picked once and then treated as settled.

A single configuration also cannot answer the question the business actually
asks — "would a different setting pay more?" — because the counterfactual was
never run.

## Decision

**No number that affects revenue is a constant. Each is an experiment with at
least two arms, running continuously, forever.**

Concretely:

1. Auction parameters resolve from the experiment layer per request
   (`experiment.go`), not from configuration. Configuration supplies only the
   fallback used when an experiment is absent or malformed.
2. Assignment is **deterministic bucketing**: hash `(experiment_key, unit_id)`
   into 10,000 fixed buckets; variants own contiguous bucket ranges.
3. Every experiment reserves a **holdout of at least 5%** on the control arm.
   `Validate()` refuses a configuration that does not.
4. Every assignment is written to the event log. An unlogged assignment is an
   unmeasurable experiment.
5. The hot path performs **no learning**. It hashes and reads a range. All
   inference happens in the cold path and returns as updated bucket ranges in
   the control plane — no deploy required.

### Why buckets rather than weights

Assigning directly from weights (`if rand() < 0.4`) reshuffles every user every
time the controller updates. That destroys session-level metrics, makes results
irreproducible, and means a player's floor changes between two games in the same
sitting.

With fixed bucket ranges, shifting 10% of traffic moves only the sessions at the
boundary. This is asserted by `TestWeightShiftMovesOnlyBoundaryTraffic`: moving
10% of traffic must reassign no more than 12% of sessions.

### Why the unit of assignment is a decision, not a detail

Choosing it wrong is the classic way to get a confident, wrong answer.

- **Session** (default) — anything a player experiences. A floor that changes
  between a player's first and second game contaminates both arms and makes
  every session metric meaningless.
- **Request** — only for effects that cannot carry across requests. Far more
  statistical power, which is exactly why it is tempting where it does not
  belong.
- **Placement** — for effects on *buyer* behaviour rather than user behaviour.
  A buyer learns a placement's floor over days; splitting that per session
  teaches them an average that exists nowhere.

### Why the holdout is mandatory

Without a slice of traffic permanently on control, you cannot measure the
controller — only the winner it selected, which is a number that always looks
good. The holdout is how we find out whether the whole apparatus earns its keep,
and it is the only defence against a controller that has quietly converged on
something harmful.

The cost is real and accepted: 5–20% of traffic knowingly runs a
worse-than-best configuration. That is the price of knowing it is better.

## Consequences

**Good**

- The counterfactual is always running. "Would X pay more?" becomes a query.
- Tuning stops being a project and becomes a background process.
- Experiments are independent by construction (the key is mixed into the hash),
  so several can run at once without correlating.
- A broken control-plane edit degrades to serving the control, never to an
  error. A bad experiment must not be able to stop the ad server.

**Bad, and accepted**

- Revenue per 1000 is heavy-tailed. Naive t-tests on it will produce confident
  nonsense; the cold path must use bootstrapped confidence intervals.
  See `docs/experimentation.md`.
- Every experiment costs statistical power. Running twenty at once on this
  site's traffic means detecting nothing. Volume, not enthusiasm, sets the
  limit on how many arms can be live.
- Multiple simultaneous experiments risk interaction effects that a per-arm
  average hides.
- More logged fields per event, so a slightly larger S3 and Athena bill.

## Alternatives rejected

- **Tune by hand, occasionally.** What we did. It produces one number, once,
  with no counterfactual and no expiry date.
- **Bandit with live weights, no holdout.** Maximises short-run revenue and
  destroys the ability to evaluate itself. A bandit that silently converges on
  a bad arm looks identical to one that converged on a good one.
- **Full multi-armed bandit in the hot path.** Violates ADR 0005 and adds state
  to a stateless serving path for a decision that changes hourly at most.
