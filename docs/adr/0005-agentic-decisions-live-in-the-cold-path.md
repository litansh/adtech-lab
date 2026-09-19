# ADR 0005 — Agentic decisions live in the cold path

**Status:** accepted · 2026-08-28

## Context

We want the fan-out to demand to be agentic. Two constraints decide where the
intelligence can physically live, and they bind differently.

### Time

```
tmax (our auction)                100 ms
network round trip                ~30 ms
left for a buyer to decide        ~70 ms

gradient-boosted tree             <1 ms
small fast LLM                    300-800 ms      11x over budget
frontier LLM                      1000-3000 ms    42x over budget
```

### Cost, using our own measured numbers

Across 106 Lab auctions the average clearing price was **$5.79 CPM**, so a won
impression is worth **$0.00579**. A buyer bidding broadly wins perhaps 5% of what
it bids on, so it pays for ~20 decisions per impression it actually wins.

```
LLM cost per bid request          ~$0.0002
decisions per impression won      20
decision cost per impression won  $0.0040
media value of that impression    $0.0058

decision cost = 69% of media value
```

At a 1% win rate -- ordinary for broad targeting -- it becomes **345%**. Media
buying runs on 15-30% margins. Either number is fatal.

For scale: that is **247x** our measured infrastructure cost per impression.

## The insight

**If time is the binding constraint, move the thinking out of the timed path.**

Split the decision in two:

```
COLD PATH   minutes to hours     reasoning, analysis, strategy
            output: a POLICY -- parameters, tables, rules

HOT PATH    <70 ms               read the policy. Apply it. Nothing else.
```

The hot path never thinks. It looks up a value some slower process already
decided. Latency added to the auction: **zero.**

The economics invert completely:

```
one agent pass per hour at $0.01, over 1000 impressions   $0.00001/impression
the same agent in the hot path                            $0.00400/impression
                                                          400x cheaper
```

That is comparable to our infrastructure cost per impression rather than 247
times it.

This is also, in substance, what the industry is doing. IAB Tech Lab's Agentic
RTB Framework cuts 600-800ms to ~100ms by **co-locating** agents to remove
network hops -- it is an infrastructure standard, not an intelligence one. And
the shipped agentic products (PubMatic AgentOS, Yahoo DSP) operate on campaign
setup and strategy, not on bid decisions.

## Decision

**Two layers, and the default layer is not an LLM.**

### Layer 1 — statistical policy (default, ~$0)

A Go job reads the event log and writes policy to DynamoDB. The ad server
already caches control-plane data in memory on a TTL, so reading policy costs
nothing extra on the hot path.

| Policy | Computed from | Hot path does |
|---|---|---|
| **Traffic shaping** -- which buyers to call | historical bid rate and timeout rate per buyer per segment | skips buyers below a threshold |
| **Floors** per placement/geo/device | the fill/revenue curve, exactly the sweep in `learning-progress.md` | reads a number |
| **Buyer bid multipliers** (Phase 4) | win rate against price paid | multiplies |

**Traffic shaping does not need an LLM, and using one would be worse.** "Which
of five buyers is worth calling?" is a table of historical rates. That is
arithmetic. An LLM would be slower, cost money, and produce a less reliable
answer than a division.

Our own measurement is the motivating case: **23.2% of buyer calls timed out.**
`buyer-slow` bids the highest of anyone and wins nothing, because it answers
late every time. A policy that stops calling it recovers that compute with no
revenue loss at all -- and no model is required to notice.

### Layer 2 — LLM reasoning, narrowly scoped (optional, gated)

Reserved for questions where judgment over unstructured input genuinely beats
arithmetic:

- *"Fill dropped 30% on mobile in Israel last Tuesday. What changed?"* -- reading
  across auction records, buyer behaviour and inventory mix
- proposing campaign targeting from a natural-language brief
- explaining a Decision Trace in plain language, as a teaching aid

**Not** for choosing which buyers to call, and **not** for setting a bid.

## Cost impact

Layer 1: **~$0.** A scheduled Go job and a few DynamoDB writes.

Layer 2: at $0.01 per pass, a few passes a day is **under $1/month** -- fits the
ceiling easily. But it requires either Amazon Bedrock or a third-party API
account, and **creating a paid third-party account is an approval gate** under
CLAUDE.md. Layer 2 is therefore not built until explicitly approved, and Layer 1
is useful without it.

## Alternatives

- **LLM in the bid path.** Rejected on both budgets above. RTBAgent (WWW'25) does
  this academically and its paper does not address latency at all.
- **No policy layer; keep calling every buyer.** Rejected: we have measured the
  waste at 23.2%.
- **Straight to Layer 2.** Rejected: it would spend money to answer a question
  that division answers better.

## When to reconsider

If a policy decision ever needs to weigh something genuinely unstructured -- a
brief, a contract, a support thread -- Layer 2 earns its place. Volume also
matters: at our scale a statistical policy over hundreds of auctions is already
close to noise, and more data makes Layer 1 better before it makes Layer 2
necessary.
