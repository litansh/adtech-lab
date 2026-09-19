# Cost attribution and FinOps

> "Cost attribution and FinOps is something we at Rise didn't fully solve. I'd
> like to know cost per publisher and per demand."

Almost nobody solves this, and the reason is structural rather than technical:
**cloud billing is organised by resource, and AdTech economics are organised by
counterparty.** One Lambda serves every publisher. One process calls every
buyer. AWS can tell you what the Lambda cost; it cannot tell you which publisher
caused it.

---

## The three wrong answers

Worth naming, because two of them are in wide use.

**1. Attribute cost in proportion to revenue.** The most common approach and the
worst. It is **circular**: a publisher that earns 2% of revenue is assigned 2%
of cost, so its margin is the platform average *by construction*. An
unprofitable publisher cannot be discovered by this method — not "is unlikely to
be", *cannot be*. If your FinOps model does this, it will report healthy margins
forever, including on the day the business stops working.

**2. Attribute by request count alone.** Better, and still wrong, because
requests differ enormously in cost. A request that short-circuits on an unknown
placement costs almost nothing. A request that fans out to five buyers and waits
100ms for three of them to time out costs far more. Counting both as "one
request" hides the expensive shape.

**3. Treat infrastructure as a fixed cost.** Defensible at our scale and
catastrophic at scale, because it is precisely how a publisher whose traffic
costs more than it earns survives for years.

## The right answer: activity-based costing from the event log

Identify the **cost drivers** — the measurable units that actually cause spend —
price each unit, and multiply by the volume each counterparty caused.

This works because our event log already records the shape of every request, not
just that it happened.

| Cost | Driver | Measured today? |
|---|---|---|
| Lambda | GB-seconds (memory × duration) | `latency_ms` ✅ |
| API Gateway | requests | count ✅ |
| DynamoDB writes | write units per event | event count ✅ |
| S3 PUT + storage | events written, bytes | event count ✅ |
| CloudFront | requests + GB egress | response bytes ⚠️ *not logged yet* |
| **Buyer calls** | calls made, and time held waiting | `auction_buyers_called`, `auction_timed_out` ✅ |
| Athena / cold path | bytes scanned | shared, see below |

The one genuinely missing input is **response bytes**. Everything else is
already there, which is the payoff of having logged the auction shape rather
than just the outcome.

## The metric that matters

Not revenue per 1000. **Contribution margin per 1000 requests:**

```
margin_per_1k = revenue_per_1k − cost_per_1k
```

A publisher with high request volume and low fill has a high `cost_per_1k` and a
low `revenue_per_1k`. Revenue-based attribution shows them as proportionally
fine. Unit-based attribution shows them as negative, immediately.

This is the whole point of the exercise, and it is why the method has to be
non-circular to be worth building at all.

## Demand-side attribution — the half nobody does

Supply-side cost attribution is at least attempted in the industry. Demand-side
is usually not attempted at all, and it is where the clearest waste lives.

**Every buyer call costs money**: egress bytes, plus the compute held open while
we wait. A buyer that times out costs the **full `tmax`** and returns nothing at
all — it is not a cheap failure, it is the most expensive possible outcome.

We already have the finding that makes this concrete. In the Lab:

- **23.2% of all buyer calls time out**
- `buyer-slow` bids the highest at $11.00, times out on 75% of calls, and
  **wins nothing**

That buyer is pure cost. Under any revenue-based view it is invisible, because
it generates no revenue to attribute cost against. Under cost-per-call it is the
most expensive thing in the auction.

The metrics:

| Metric | Question it answers |
|---|---|
| **cost per buyer call** | what does asking this buyer cost |
| **cost per win** | what did each win from them actually cost to obtain |
| **timeout cost share** | how much are we spending on calls that cannot return in time |

**Consequence worth stating:** traffic shaping — deciding which buyers to call
for which opportunity — is a **FinOps decision** as much as a latency one. That
reframing is the practical output of this document.

## Shared costs: do not force-allocate them

The control plane, monitoring, alarms, the cold-path jobs themselves — these
serve everyone and are caused by no one.

Spreading them across publishers by some ratio *feels* rigorous and destroys the
credibility of the whole model, because the ratio is arbitrary and everyone
knows it. Report them separately:

```
platform overhead = shared cost / total revenue
```

Track that percentage over time. It is a real and useful number, and it is
honest in a way that an allocated-per-publisher version would not be.

## Why CloudFront logs are an input, not the answer

CloudFront access logs give requests and bytes per distribution, splittable by
host and path. That is genuinely useful for the **publisher site** (static
assets, egress) and it is the cheapest way to get per-site egress.

But the ad server's real cost is Lambda time and buyer calls, and CloudFront
cannot see either. It does not know a request fanned out to five buyers and
waited 100ms. So: use CloudFront logs for egress attribution, and the event log
for everything else. Neither alone is sufficient.

## Accuracy, stated honestly

This model will not reconcile to the AWS bill to the cent, and claiming it does
would be the first step toward nobody trusting it.

- Unit prices are list prices; free-tier allowances and rounding are not modelled.
- Lambda bills in 1ms increments with a minimum; our `latency_ms` is measured
  in-handler and excludes cold-start and runtime overhead. **Attributed cost is
  therefore an underestimate.**
- Shared costs are excluded by design, not by omission.

What it *is* good for is **relative** comparison: which publisher, placement or
buyer is expensive relative to the others, and which has negative margin. Those
rankings are robust to unit-price error, because the error applies to everyone.

**Use it for decisions about relative worth. Do not use it for invoicing.**

## The FinOps agent

Added as the sixth agent in [agent-fleet.md](agent-fleet.md).

| | |
|---|---|
| **Decides** | which publishers and buyers are worth serving, and at what request rate |
| **Owns** | contribution margin per 1000, per publisher / placement / buyer |
| **Vetoes** | Yield — revenue that costs more than it earns is not revenue |
| **Vetoed by** | Reliability, Integrity, Product |

**Why it is not just part of Reliability.** Reliability asks *"can we afford to
stay up, and are we inside the ceiling?"* — its levers are throttles and the
fuse. FinOps asks *"is this particular traffic worth serving at all?"* — its
levers are dropping a buyer, reshaping fan-out, or repricing a publisher. Same
currency, different question, different feedback signal.

**Arbitration order**, updated:

```
1. Reliability   stay up, stay inside the cost ceiling
2. Integrity     never bill for what we should not bill for
3. Product       never trade the product for a quarter of revenue
4. FinOps        revenue that costs more than it earns is not revenue
5. Yield         then, maximise revenue
6. Demand        then, minimise the cost of getting it
```

FinOps sits directly above Yield because that is the conflict it exists to
settle: Yield optimises gross revenue and will happily buy it at a loss.

## Implementation

`tools/finopsctl` computes all of the above from the event log. Unit prices live
in `tools/finopsctl/prices.json` so they can be corrected without a code change,
and every output states that it excludes shared cost.
