# Traffic filtering: four layers, four different answers

> "Filtering of stupid traffic / WAF / product / business should be handled
> differently" — and it should. This document is the result of thinking that
> through.

The mistake almost every implementation makes is having **one** bot decision
and applying it everywhere. It produces two expensive failures at the same
time: crawlers we *want* get blocked, and traffic we should never bill for gets
billed.

The fix is to notice that "is this a bot?" is not one question. It is four
questions, asked at four layers, with four different correct responses.

| Layer | Question | Wrong answer costs | Response |
|---|---|---|---|
| **1. Edge / WAF** | Is this abusing our infrastructure? | the AWS bill | rate-limit, block |
| **2. Product** | Do we *want* this visitor? | discovery and SEO | allow, always |
| **3. Business** | Should anyone be charged for this? | advertiser trust, refunds | serve unpaid |
| **4. Cold path** | Was that earlier judgement right? | slow, correctable | revoke, re-bill, retrain |

---

## Layer 1 — Edge / WAF: protecting the bill

**Question:** is this volume going to cost us money or take the site down?

This is a **cost and availability** decision and has nothing to do with
advertising. It belongs at CloudFront, *before* Lambda, because a request
rejected at the edge costs a fraction of one that reaches compute. Our exposure
here is the reason: this stack has **no hard monetary cap**, and Lambda account
concurrency is 10 with no reservation possible on the Free plan.

Belongs here:
- rate limiting per IP and per session
- request floods, obvious L7 abuse
- malformed or oversized bodies
- geographies we knowingly do not serve

Explicitly **not** here:
- anything about who deserves to be billed. A WAF has no idea what an
  impression is worth, and by the time it could tell, it has already made the
  cheap decision it exists to make.

The rule: **Layer 1 blocks for cost, never for quality.** A visitor rate-limited
here is one we could not afford, not one we disapprove of.

## Layer 2 — Product: who we want on the site

**Question:** do we want this visitor to reach the content at all?

The counter-intuitive answer for most non-human traffic is **yes, enthusiastically**.

- `Googlebot`, `Bingbot` — how anyone finds the site at all.
- `GPTBot`, `ClaudeBot`, `PerplexityBot`, `OAI-SearchBot` — increasingly how
  people find anything. A site invisible to assistant search in 2026 is
  invisible to a growing share of its audience.
- Accessibility tools and reader agents — a real person is behind them.

Blocking these to "stop bots" trades away discovery to prevent a billing
problem that Layer 3 solves for free. This is the most common and most
expensive version of the mistake, and it is usually made by someone who thinks
they are protecting revenue.

`robots.txt` is the honest tool here, and it is a **product** decision — what we
want indexed — not a fraud control. It has no enforcement, and anything willing
to ignore it was never going to be stopped by it.

## Layer 3 — Business: who gets charged

**Question:** should an advertiser pay for this?

This is the ad server's decision (`filter.go`), and it is entirely separate from
whether the visitor is welcome. Three outcomes, not two:

```
human           -> full auction, billable
declared_agent  -> served, logged, NOT billable
suspected_ivt   -> house ad only, no buyer ever charged
```

The distinguishing line is **declaration, not automation**. A crawler that
identifies itself is a good citizen. A headless browser spoofing an iPhone from
a datacentre is not. The difference is honesty, not technology.

Two rules that fall out of this and are easy to get wrong:

**Declaration must beat suspicion.** An agent that declares itself *and* trips
every heuristic is still classified as declared. If honesty is scored worse than
silence, nobody declares themselves twice, and we lose the only signal that
actually works. This is asserted by `TestDeclarationBeatsSuspicion`.

**We serve suspected IVT rather than blocking it.** A block is a free oracle: it
tells the operator exactly which of their signals we detect, and they iterate
until we stop blocking. A house ad tells them nothing, costs us nothing, and
keeps our detection private. Blocking is a Layer 1 tool; Layer 3 declines to
pay instead.

Non-billable traffic also **never reaches the auction**. Calling buyers for an
impression we have already decided is unbillable spends their `tmax` budget on
inventory they must never be charged for, and a bid we would refuse to honour is
worse than a bid never requested.

## Layer 4 — Cold path: the judgement we could not make in 5ms

**Question:** was the Layer 3 call correct, given what we know now?

Layer 3 sees one request and has single-digit milliseconds. That is a genuinely
weak position, and pretending otherwise is how pre-bid filtering acquires false
confidence. The strongest evidence arrives *after* the decision:

- viewability — was the ad ever actually on screen?
- time-on-page, replay rate, input entropy
- a click that arrives before the creative could have rendered
- one "session" appearing from twelve countries
- population statistics: a placement whose CTR is 40× every other placement

None of this is available pre-bid. All of it is available an hour later, for
almost nothing, in the cold path — which is
[ADR 0005](adr/0005-agentic-decisions-live-in-the-cold-path.md) again: the hard
judgement lives outside the timed path.

Layer 4 can **revoke billability retroactively**. That is why the event log
carries `traffic_class`, `billable` and `traffic_reasons` on every event rather
than a single boolean: "why was this not billed?" is a question a buyer is
entitled to ask months later, and it can only be answered from the log.

---

## Why this matters more in the agentic era

Historically, "non-human" and "worthless" were close enough to synonyms that
conflating them cost little. That is ending.

An agent acting for a real person with a real budget is **not fraud**. It is a
different product. Display advertising against it is close to worthless — no
human eye is involved — while the *intent* behind it may be worth more than any
banner impression the site will ever serve. A shopping agent comparing prices on
behalf of a buyer is high-value traffic that should be monetised through an
entirely different mechanism than a CPM.

A pipeline with one bot flag cannot express that. It has to answer "block or
allow", and both answers are wrong.

The four-layer split is what makes the right answer sayable:

> Let it in (Layer 2), don't charge a display advertiser for it (Layer 3), count
> it separately so we can see how much of it there is (Layer 4) — and decide
> later whether the intent is worth selling in a different way.

**Measuring the volume of declared agent traffic is, on its own, one of the more
interesting numbers this project can produce.** It is the leading indicator for
whether agentic monetisation is worth building at all, and almost nobody is
counting it, because almost everybody is blocking it.

## Current state, honestly

| Layer | Status |
|---|---|
| 1. Edge / WAF | **partial.** API Gateway per-route throttling and the Cost Fuse exist. No AWS WAF — it has a fixed monthly cost that is hard to justify at current traffic |
| 2. Product | **done.** `robots.txt` allows search and assistant crawlers deliberately |
| 3. Business | **done.** `filter.go`, three-way classification, tested |
| 4. Cold path | **not built.** Events carry the fields for it; no revocation job exists yet |

Layer 4 is the honest gap. Everything needed to build it is already in the log.
