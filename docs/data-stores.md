# Data stores: what to use, and why the answer differs by scale

> "At Rise we use a lot of Aerospike and now Valkey and BigTable… we won't need
> and won't be able to use Aerospike, but think about it, I'd love to hear a
> best practice solution." … "We also use BigQuery for data science which I
> claim we don't need."

The mistake this document exists to prevent: treating "which database?" as one
question. **AdTech has four distinct data problems**, they have almost nothing
in common, and the reason a company ends up running Aerospike *and* Valkey *and*
BigTable *and* BigQuery is usually that each was chosen correctly for a
different one.

---

## The four problems

| # | Problem | Access pattern | Latency budget | Durability if lost |
|---|---|---|---|---|
| 1 | **Hot lookup** — profile, segments, seller config | random point read by key | **< 1ms p99** | rebuildable |
| 2 | **Hot counters** — pacing, budget, frequency cap | read-modify-write by key | **< 1ms p99** | costly but survivable |
| 3 | **Event log** — every request, bid, impression | append-only, never read by key | write must not block | **must not lose** |
| 4 | **Analytics** — reporting, experiments, data science | full scans, aggregations | seconds to minutes | derived |

Trying to serve any two of these with one store is where the pain comes from.
Problem 1 and 2 look identical and are not: a read is idempotent and cacheable,
a counter is neither.

---

## Problem 1 — Hot lookup

**What the industry uses: Aerospike.** Not fashion. Aerospike's design point is
specifically *random reads from SSD with predictable tail latency at very high
QPS*, which is almost exactly the bidder's access pattern. It keeps the index in
RAM and the data on flash, so you get RAM-like p99 at flash-like cost — the
difference between fitting a 500GB profile store in memory and not.

The alternative at that scale is "put it all in RAM", which is Redis/Valkey and
costs several times more per GB.

**Why Valkey is appearing now:** Redis's licence change in 2024 pushed the
industry to the Valkey fork. For workloads that genuinely fit in RAM and want
rich data structures, it is the better fit — and it is *simpler* than Aerospike,
which matters more than people admit.

**What we use here: DynamoDB with a 60s in-process cache.**

And the honest reason is not that DynamoDB is better. It is that **our QPS is
approximately zero.** The control plane is a few hundred rows that change
rarely. A 60-second in-process cache means the hot path issues ~zero reads, so
the store's latency characteristics do not matter at all — the cache is the
database, and DynamoDB is where it is reloaded from.

> At 50B events/day this is Aerospike on dedicated hardware.
> At our scale it is a Go map behind a TTL, and anything else is a costume.

## Problem 2 — Hot counters

The genuinely hard one, and the one people underestimate.

A budget counter must be correct enough that a campaign does not overspend, and
fast enough to not blow the bid timeout, **across many concurrent bidders**. You
cannot have both. The choice is which to give up, and by how much:

- **Strict consistency** (conditional writes, single writer): correct, and adds
  a round trip to every bid. Nobody does this at scale.
- **Bounded over-delivery** (local counters, periodic reconciliation): fast, and
  overspends by a bounded amount. **This is what everyone actually does**, and
  the honest ones say so in the contract.

Aerospike and Valkey both serve this well, and the store is not really the
decision — the *reconciliation interval* is. That interval is your over-delivery
bound, and it belongs in a document, not in someone's head.

**What we use: DynamoDB atomic counters plus a cached read, accepting bounded
over-delivery.** Documented in `state.go` and `docs/money-flow.md`, along with
the separation that matters more than the store: **`serving_budget_state` is not
`billing_ledger`.** Serving state may be approximate because it is a control
signal; the ledger may not, because it is money. Conflating them is how
publishers and advertisers end up arguing about numbers that were never meant to
agree.

## Problem 3 — Event log

**Write path, never a read path.** The requirement is that a slow consumer can
never slow a bid.

Industry: Kafka → object storage, or a managed firehose.

**What we use: buffered gzipped NDJSON to S3, hourly partitions.** We started
with Kinesis Firehose and discovered it is unavailable on the AWS Free plan, so
`sink_s3.go` buffers per invocation and flushes at the end. That is a downgrade
in exactly one way — an invocation that dies loses its buffer — and it is
recorded rather than glossed.

Kafka would be the wrong answer here even with budget: we have one producer and
one consumer, and Kafka's value is in fan-out and replay we do not need.

## Problem 4 — Analytics

Here is where the BigQuery question lives.

**Three tiers, and most companies need fewer than they run:**

| Tier | What it is for | Examples |
|---|---|---|
| **Query-over-object-storage** | ad-hoc SQL over what you already stored | Athena, BigQuery external tables, Trino |
| **Warehouse** | modelled, joined, governed data; heavy repeated aggregation | BigQuery native, Snowflake, Redshift |
| **Real-time OLAP** | sub-second slice-and-dice for dashboards over billions of rows | Druid, ClickHouse, Pinot |

**What we use: S3 + Athena with partition projection.** Partition projection
matters: without it Athena hits the Glue metastore for partitions and gets slow
and expensive; with it, partitions are computed from the path template and the
metastore is barely touched. Cost is per byte scanned, which the hourly
partitioning keeps to nearly nothing.

### Does a company like Rise need BigQuery?

Reasoning from the shape of the problem rather than from any look at their
systems — and the answer is genuinely *it depends on which of three things they
are using it for*:

**1. Ad-hoc SQL over event data.** This does **not** need a warehouse. External
tables over object storage do it, and the cost model is better because you pay
per scan rather than for storage in a proprietary format. If BigQuery is being
used only this way, your instinct is right.

**2. Repeated heavy aggregation over the same joined data.** This is where a
warehouse earns its cost. If the same 90-day joined aggregate is recomputed
hourly for reporting, scanning raw object storage each time is *more* expensive
than a warehouse's materialised, columnar, clustered copy. The break-even is
about **repetition**, not volume.

**3. Data science and ML feature engineering.** Iterative, exploratory,
join-heavy, and needs to be fast enough that an analyst does not lose their
train of thought. This is the strongest case for a warehouse, and it is a
*human productivity* argument rather than a technical one — which is why it is
often dismissed by engineers and defended by the people doing the work.

**The diagnostic question:** what fraction of BigQuery spend comes from
scheduled queries versus interactive ones, and how much of the scheduled spend
recomputes something that could be materialised once? If most of it is
scheduled and unmaterialised, the answer is not "drop BigQuery" — it is "the
modelling layer is missing", and dropping BigQuery would make it worse.

**Where it is genuinely redundant:** running a warehouse *and* a real-time OLAP
store *and* query-over-object-storage, with the same data in all three, because
each was added for one use case and none was ever removed. That is the common
failure, and it is an organisational one — nobody is rewarded for deleting a
pipeline.

For **us**: no warehouse. Not "not yet" — the data is one wide event table, the
queries are ad-hoc, and there is no second dataset to join to. A warehouse
solves a problem we do not have.

---

## Our stack, and the honest reason for each

| Problem | Ours | Industry at scale | Why the difference is legitimate |
|---|---|---|---|
| Hot lookup | DynamoDB + 60s cache | Aerospike | Our QPS is ~0; the cache is the database |
| Hot counters | DynamoDB atomic + cached read | Aerospike / Valkey | Same, plus we accept documented bounded over-delivery |
| Event log | gzipped NDJSON → S3 | Kafka → object storage | One producer, one consumer; no fan-out to justify Kafka |
| Analytics | Athena + partition projection | BigQuery / Snowflake / Druid | Ad-hoc only, one table, no joins |

**The rule this table encodes:** every one of these is the simplest thing that
teaches the concept, and each row states what it would be at 50B events/day. We
are not pretending our choices are what a real SSP should run. We are recording
*why* the real answer differs, which is the part that transfers.

## When each of ours would break

Written now, so the trigger is a measurement rather than a feeling:

- **DynamoDB + cache** breaks when the control plane must change faster than the
  60s TTL, or grows past what fits in a Lambda's memory. → Valkey.
- **Atomic counters** break when over-delivery exceeds the documented bound
  under concurrency. → a real counter store, and a stricter reconciliation
  interval.
- **S3 buffering** breaks when losing an invocation's buffer stops being
  acceptable — which is *the moment real money depends on the log*. → a managed
  stream with at-least-once delivery and idempotent consumers.
- **Athena** breaks when the same aggregate is recomputed often enough that
  per-scan billing exceeds a materialised copy. → a modelling layer first, a
  warehouse only if that is not enough.

The last one is the same break-even as the BigQuery question above, which is not
a coincidence: it is the only question in this document that is really about
economics rather than latency.
