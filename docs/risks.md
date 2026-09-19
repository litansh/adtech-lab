# Risks and How We Avoid Them

## 1. Too Expensive

| Risk | Mitigation |
|---|---|
| A public endpoint is abused and generates a large bill | Layered controls: CloudFront flat-rate plan (no overage charges), WAF IP rate limiting, API Gateway route throttling, Lambda reserved concurrency, application-level rejection before any billable downstream write. Quantified worst case in [cost-model.md](cost-model.md) |
| Treating one control as "the ceiling" | Reserved concurrency bounds throughput, not spend. Budgets detect, they do not prevent. **There is no hard monetary cap anywhere in this stack** — the one contractual cap (CloudFront flat-rate) is unavailable on the Free plan |
| Static-path flood bypassing the Cost Fuse | Accepted, stated tail risk. Static routes are deliberately excluded from the fuse so a traffic spike cannot take the games offline. Bounded by IMMEDIATE anomaly alerts at $5 and budget alerts from $20; the response is to add WAF at that point |
| Free plan expiring mid-project (2027-02-08) | A calendar constraint, not a cost one. Phases 1-5 fit; Phase 6+ needs an account-plan decision well before February |
| Abuse billing for hours or days before anyone notices | The **Cost Fuse**: four independent tiers (per-minute rate, per-day volume, actual spend, plus a CloudFront alert) trip a fail-closed block on `/ad*` and `/event*` within ~2-3 minutes. Static games site stays online. Manual reset only |
| The fuse itself failing | Tiers use *different signals* rather than three variants of one, so a single blind spot does not disable all of them. Trip is tested end to end as a Phase 1 exit criterion. Residual risk is stated, not hidden |
| A new endpoint added later without a throttle or fuse coverage | `adlabctl fuse status` makes coverage checkable. Any new public dynamic route is a checklist item on the PR |
| CloudWatch or Firehose costs scale silently with traffic | Log sampling, record batching, 7-day retention, and both lines are visible in the cost model at each scenario |
| DynamoDB writes scale linearly | Batched counter flush, with the over-delivery trade-off made explicit |
| A "small" always-on service gets added | The Architecture Honesty Rule: what concrete problem do we have *today* that requires this? |

## 2. Too Complicated

| Risk | Mitigation |
|---|---|
| Building many services before serving one ad | Vertical slices. Phase 1 has one service |
| Reaching for Kubernetes or Kafka because the industry uses them | Explicitly out of scope. The comparison is documented instead: "at 50B events/day this would be X; at our scale it is Y" |
| Implementing all of OpenRTB | Phase 5 implements a subset, chosen by what we actually need |

## 3. Not Representative of Modern AdTech

The most insidious category, because the software still works.

| Risk | Mitigation |
|---|---|
| An auction that always has a winner | Phase 3 deliberately simulates no-bids, timeouts, and below-floor bids |
| No latency pressure | `tmax` modelled explicitly from Phase 4; p50/p95/p99 measured from Phase 1 |
| Synthetic-only traffic | Phase 1 runs a real browser against a real domain from day one |
| Ignoring discrepancies | Phase 4 builds an independent buy-side counter specifically so the numbers diverge and we have to explain why |
| Learning a 2018 ecosystem | `standards-status.md` re-verified at the start of each phase, with sources |
| Conflating gross media handled with revenue | Separate ledgers from the first line of code |
| Simplifications silently hardening into beliefs | Every component doc has a mandatory "what we simplified" section; conceptual boundaries stated even where the market blurs them |

## 4. Hard to Monetise

| Risk | Mitigation |
|---|---|
| No network or affiliate programme approves a brand-new publisher with negligible traffic | Research actual approval requirements **before** building anything for Phase 6. Assume rejection is the default outcome |
| Our ad server is pushed out of the serving path by whatever demand we connect | An explicit evaluation criterion for every monetisation option: does our ad server remain in the path, and what do we still learn if it does not? |
| Chasing small revenue damages the learning project | Revenue is a secondary goal. Spending $500 on traffic to earn $20 is a failure, not a start |

## 4b. The Publisher Becoming an MFA Site

The mini-games Publisher creates a risk that a generic content page did not: the temptation to optimise it for ad opportunities rather than for players.

| Risk | Mitigation |
|---|---|
| Adding placements, refresh or interstitials because they raise impressions | The **Product Quality Rule**: the site must be worth visiting with advertising removed. Every monetisation change is measured against `session_duration`, `game_completion_rate` and `replay_rate` in the same experiment |
| Fragmenting the product to manufacture page views | Named explicitly in [cost-model.md](cost-model.md): high engagement and high ad-opportunity volume are in tension, and that tension is what produces slideshow galleries. We measure it rather than resolve it by instinct |
| The games project expanding until the AdTech work never happens | Phase 1 is one game, one placement, one ad server. Accounts, multiplayer, leaderboards, more games and a framework rewrite are all explicitly out of scope without a product reason |
| Buying traffic to make the numbers look better | Traffic acquisition is a Phase 6+ decision with its own gate, and only if `TAC < advertising revenue` on real, policy-compliant traffic |

## 5. Legal and Privacy

| Risk | Mitigation |
|---|---|
| Assuming "no cookies" means "no privacy obligations" | [privacy-baseline.md](privacy-baseline.md) applies from Phase 1: IP and User-Agent processing is addressed even with no identifier |
| Introducing an identifier casually to enable a feature | Gated decision with a written purpose, scope, lifetime, notice update, and consent analysis before implementation |
| Israeli Privacy Protection Law Amendment 13 obligations | In force since August 2025. Flagged for professional assessment before Phase 6, not assumed either way |
| GDPR exposure from EU/EEA visitors | Assessed before monetisation or any expansion of processing purposes |

## 6. Misleading from a Learning Perspective

| Risk | Mitigation |
|---|---|
| Working code mistaken for understanding | Validation questions after each milestone; the Decision Trace is a first-class feature |
| My own explanations being wrong or outdated | Primary sources cited; standards re-verified each phase; corrections recorded in the documents rather than quietly overwritten |
| Measurement that can be trivially faked | Signed, expiring tracking tokens and idempotent event IDs from Phase 1 — otherwise every metric built on top is fiction |

---

## 7. Environment Isolation — corrected

The first draft claimed the Lab and Production "never share accounts." That is not accurate for this project: **the same personal AWS account will host both, at least initially.** Requiring separate accounts would be a rule we immediately break.

The real requirement is isolation of everything that could let synthetic activity contaminate real activity:

```text
Separate resources          distinct DynamoDB tables, S3 prefixes, Firehose streams,
                            Lambda functions, API stages
Separate namespaces         every identifier and every event carries env=lab|prod;
                            no query, report, or ledger ever mixes them
Separate IAM roles          the Lab role cannot read or write Production data,
                            and cannot reach monetisation credentials at all
Separate data and streams   synthetic events never enter the Production billing ledger
Separate credentials        monetisation partner credentials exist only in Production
                            and are never present in any Lab execution context
Separate egress             no Lab component may call an external partner endpoint
```

**The absolute rule:** synthetic events must never reach a real monetisation partner, and synthetic activity must never appear in any figure reported to an advertiser, a publisher, or a partner.

Moving to separate AWS accounts (via AWS Organizations) is a reasonable later step and would make several of these controls structural rather than procedural. It is not required to start, and pretending it is already true would be worse than stating the actual arrangement.

## 8. Absolute Prohibitions

Never, under any circumstance, against a real monetisation platform or partner:

```text
fake impressions          fake clicks              bot-generated traffic
incentivised clicks where prohibited               misrepresented inventory
hidden ads                layouts engineered for accidental clicks
traffic laundering        synthetic conversions    policy circumvention
```

The Lab may simulate all of these **against our own systems**, for the purpose of learning to detect them. That is the entire reason the Lab exists.
