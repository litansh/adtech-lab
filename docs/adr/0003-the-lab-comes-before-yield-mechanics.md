# ADR 0003 — The Lab comes before yield mechanics

**Status:** accepted · 2026-08-28

## Context

The roadmap had Phase 2 as *pacing, frequency capping, bid floors, viewability*,
then Phase 3 as *multiple buyers and a first-price auction*.

Reviewing the live system before starting Phase 2 shows that order does not work.
What actually exists today:

```
li-acme-cpm        CPM  $4.00   priority 5   targets IL, desktop
li-northwind-us    CPC  $0.45   priority 5   targets US  <- never eligible on our traffic
li-house-fallback  CPM  $0.00   priority 0   fallback
```

On a typical request there is **exactly one eligible paying line item.**

Each Phase 2 concept turns out to depend on something Phase 3 provides:

| Concept | What it needs | Do we have it? |
|---|---|---|
| **Bid floor** | Competing bids to filter. A floor rejects demand below a price | **No.** With one buyer a floor is just another eligibility check — there is nothing to filter, and no price at which fill changes |
| **The floor/fill trade-off** | Demand whose willingness to pay *varies* | **No.** Fill is currently binary: Acme is eligible or it is not |
| **Pacing** | Enough volume for a delivery curve to exist | **No.** A handful of requests per day has no curve to pace against |
| **Frequency capping** | Users, repeatedly | **No.** And production deliberately has no persistent identifier |
| **Viewability** | Impressions, in volume | **No.** |

Building all of that now would mean writing machinery we cannot observe working,
and then "verifying" it against data too thin to show whether it is correct.
That is the failure mode the project exists to avoid: working software mistaken
for understanding.

## Decision

**Swap the order. Build the Lab first.**

Phase 2 becomes: **synthetic traffic + a population of competing buyers + a
first-price auction.** Concretely:

- a traffic simulator producing sessions, games and ad requests at controllable
  volume, with varied geo, device and context
- several independent buyers with *different* willingness to pay, different
  targeting, and deliberately imperfect behaviour: no-bids, slow responses,
  budget exhaustion
- a real first-price auction with a clearing price
- synthetic `user_id`s, so frequency capping is learnable in the Lab exactly as
  `privacy-baseline.md` already requires

Yield mechanics — floors, pacing, viewability — move to Phase 3, where the
conditions to observe them will exist.

## Why this is better, not merely different

**Every Phase 2 concept becomes measurable rather than theoretical.** A floor
raised from $1.50 to $3.00 will visibly reject the buyers below it and visibly
change fill, because there will be buyers at both prices.

**It removes the traffic dependency.** Real players are months away and are goal
2's problem. Learning must not be blocked behind an audience.

**It fires the Cost Fuse for free.** A traffic simulator *is* the load test the
Definition of Done already requires, so the fuse gets measured rather than
estimated as a side effect of work we want anyway.

**It brings the highest-value concepts forward.** Auctions, clearing prices,
no-bids and win rate are closer to the centre of AdTech than pacing is. With
five months on the account, the ordering matters.

## Alternatives

- **Keep the original order.** Rejected: it builds unobservable machinery.
- **Do the load test on its own first.** Rejected: it is a subset of this, and
  doing it separately means generating traffic twice.
- **Jump straight to the mini DSP (Phase 4).** Rejected: the auction has to exist
  before a buyer has something to bid into.

## Cost impact

Synthetic traffic costs real money — Lambda invocations, DynamoDB writes, S3
objects. It must be bounded and it must be tagged `env=lab`, never mixed with
production figures. Expect single-digit dollars per month of deliberate load,
inside the existing ceiling and covered by credits.

## When to reconsider

If real traffic arrives sooner than expected, some of the simulation becomes
unnecessary — but the competing-buyer population does not. Real visitors do not
create competing demand; only buyers do.
