# Portable agents

> "I want all parts to be generic — I can take each part, a demand agent, and
> add it in a few steps to Rise or any other company. Even with a tiny
> integration, read-only reporting mode, and then…"

That "and then…" is the important part of the sentence, and this document turns
it into a defined ladder.

The goal: **an agent should be droppable into another company in an afternoon,
start in a mode that cannot do harm, and earn its way up to acting.**

---

## Why the current tools are not portable

`yieldctl`, `integrityctl` and `finopsctl` are each welded to three things:

1. **Our event schema** — they read `auction_buyers_called`, `price_cpm`,
   `session_id` by name.
2. **Our storage** — local NDJSON, or gzipped NDJSON in our S3 layout.
3. **Our control plane** — bucket ranges in our DynamoDB JSON.

Any one of those makes the tool useless elsewhere. All three make it a rewrite.

The fix is not "make it configurable" in the usual sense. It is to notice that
**each agent needs surprisingly few facts**, and to make those facts the
interface.

## The contract: what each agent actually needs

This is the whole portability argument. An agent that needs six fields can be
integrated by producing six fields.

| Agent | Facts it needs | How common is this data? |
|---|---|---|
| **FinOps** | counterparty id, request count, compute time, downstream calls, revenue | **Very** — every ad system has these |
| **Demand** | buyer id, calls, bids, timeouts, wins, revenue, latency | **Very** — any SSP logs this |
| **Integrity** | session id, impression + click timestamps, country, an engagement signal | **Common** — the engagement signal is the weak point |
| **Yield** | per-event **experiment arm assignment**, revenue, guardrail metrics | **Rare** — see below |
| **Reliability** | spend, error rate, latency percentiles, capacity limits | Common, but very platform-specific |
| **Product** | engagement, retention, return rate | Entirely product-specific |

**The honest ranking.** FinOps and Demand port almost anywhere. Integrity ports
with one caveat. **Yield usually does not port at all** — not because the
algorithm is special, but because it needs the arm assignment written on every
event, and most companies have no experimentation layer in the serving path.
Without that, the agent has nothing to compare. See
[ADR 0006](adr/0006-every-money-number-is-a-variant.md): the prerequisite is not
the controller, it is the assignment.

Reliability and Product are the least portable and should be expected to be
rewritten per company. That is fine — they are also the two most likely to
already exist in some form.

## The integration: a mapping file, not a fork

The canonical schema is deliberately tiny. Integration is a JSON file that says
which of your fields mean what:

```json
{
  "source": "ndjson",
  "fields": {
    "event_type":   "event",
    "counterparty": "publisher_id",
    "revenue_micros": null,
    "revenue":      "price_cpm",
    "revenue_scale": 0.001,
    "compute_ms":   "latency_ms",
    "downstream_calls": "auction_buyers_called",
    "buyer_id":     "buyer_id"
  },
  "event_values": {
    "request":    "ad_request",
    "impression": "impression",
    "click":      "click"
  },
  "filters": { "env": "production" }
}
```

At another company the same file names *their* columns. Nothing else changes —
no fork, no code edit, no rebuild. That is the "tiny integration".

**Fields the agent needs but you cannot supply** are reported as missing, with
the consequence stated ("no compute time — Lambda cost will read as zero, and
the cost ranking will understate compute-heavy counterparties"). The agent
degrades and says how, rather than silently producing a confident wrong number.

## The maturity ladder

The part that makes this safe to try. Every agent supports every stage, and the
stage is a flag — not a different build.

| Stage | Name | What it does | Can it cause harm? |
|---|---|---|---|
| **0** | **Observe** | reads, reports, writes nothing anywhere | **No.** Read-only credentials suffice |
| **1** | **Recommend** | emits a proposal a human applies | No — a human is the actuator |
| **2** | **Shadow** | computes what it *would* have done, logs it beside what actually happened | No — and this is where trust is earned |
| **3** | **Bounded act** | applies changes within hard limits, with a holdout | Yes, bounded |
| **4** | **Act** | full autonomy inside guardrails | Yes |

### Stage 0 is the entire pitch

Read-only credentials, one mapping file, one command, a report. No write path
exists in the binary at that stage. Nothing to review beyond "can this read our
logs?"

For most organisations the answer to "can we try an autonomous yield agent?" is
no, and the answer to "can we run a read-only report over logs we already keep?"
is yes. Stage 0 converts the second answer into evidence for the first.

### Stage 2 is the one everybody skips

**Shadow mode is the point of the ladder.** The agent computes its decision,
does not apply it, and records the counterfactual next to what actually
happened. After a few weeks you can answer, with data rather than assertion:

> *"Would this agent have made us money? By how much? How often would it have
> been wrong, and how badly?"*

Nobody skips Stage 0 because it is obviously safe. Everybody wants to skip
Stage 2 because the agent already "works" — and Stage 2 is the only stage that
produces evidence a sceptical VP can act on. An agent that cannot show a shadow
record has not earned Stage 3.

This is also the honest answer to "how do we know the fleet is worth it?", which
is the same question the permanent holdout answers once it is running.

### Promotion criteria, written before starting

So that promotion is a measurement and not a mood:

| Promotion | Requires |
|---|---|
| 0 → 1 | the report is correct on data a human has spot-checked |
| 1 → 2 | a human has applied ≥ 5 proposals and none was wrong |
| 2 → 3 | ≥ 4 weeks of shadow showing positive expected effect, and no guardrail breach the agent failed to catch |
| 3 → 4 | ≥ 8 weeks bounded, with a permanent holdout proving the agent beats its control |

**Demotion is automatic and needs no meeting:** any guardrail breach the agent
caused drops it one stage.

## What this costs us

Not free, and worth stating:

- A canonical schema is a **lowest common denominator**. Agents lose access to
  facts specific to our system unless those facts are added to the contract.
- A mapping layer is indirection, and indirection hides bugs. Mitigated by
  making the loader report exactly which fields resolved, which defaulted and
  which are missing — every run, not on request.
- Five stages is more code than one, and four of them are code that
  deliberately does nothing.

The last point is the one to be honest about: **most of this machinery exists to
make the agents adoptable, not to make them better.** That is a legitimate goal
— an agent nobody is allowed to run has no value — but it is not the same goal
as accuracy, and conflating them is how you end up with a very portable tool
that is wrong everywhere.

## Feasibility, per agent, stated plainly

| Agent | Port to another company | Honest assessment |
|---|---|---|
| **FinOps** | **Easy** | Needs counterparty + activity counts. A day, mostly spent agreeing what a "counterparty" is |
| **Demand** | **Easy** | Any SSP logs calls, timeouts and wins. The data is usually already in a dashboard |
| **Integrity** | **Moderate** | Impression/click timestamps are universal; a reliable *engagement* signal often is not, and two of the five rules depend on it |
| **Yield** | **Hard** | Requires per-event arm assignment. If it does not exist, the agent cannot run — building the assignment layer is the real project, and the controller is the easy part afterwards |
| **Reliability** | **Rewrite** | Deeply platform-specific |
| **Product** | **Rewrite** | Entirely product-specific |

**The most useful thing to carry into another company is not a tool. It is the
list of six facts each agent needs** — because that list is short enough to
check against an existing schema in an afternoon, and the answer tells you
immediately whether the agent is weeks away or months.
