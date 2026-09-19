# Glossary — Not a Dictionary, but "Why I Care"

Each entry: **what it is -> why I care -> the trade-off -> the conclusion**.

Where a term is used with different definitions across the industry, that is stated rather than papered over.

---

## Ad Request / Ad Opportunity / Impression

- **Ad Request** — a request to the ad server for one or more ads.
- **Ad Opportunity** — a single slot that could be filled. One request may contain several.
- **Impression** — an ad was returned, rendered, and counted.

**Why I care:** every metric below is a ratio between these. Confuse them and the reports are fiction.

**Conclusion:** always measure the whole funnel: `requests -> opportunities -> responses -> rendered -> impressions -> viewable impressions`. The drop between any two steps is a diagnosis.

---

## Fill Rate — and why there is no single definition

There is **no universal denominator** for "fill rate". Different platforms mean different things by it, which is a common source of cross-platform reporting arguments.

**Our project defines four separate metrics, explicitly:**

```text
request_fill_rate      = responses_with_an_ad / ad_requests
opportunity_fill_rate  = filled_opportunities / ad_opportunities
render_rate            = impressions / responses_with_an_ad
viewability_rate       = viewable_impressions / impressions
```

We will always name which one we mean. Industry platforms may compute against ad requests, ad opportunities, "eligible" requests only, or requests net of errors and blocked traffic.

**Why I care:** inventory that does not fill earns nothing.

**The trade-off:** raising the floor raises the average price of what sells but reduces how much sells.

**Conclusion:** a higher CPM does not imply higher revenue. What matters is revenue per opportunity — roughly `price x fill`. This is the most common yield-management mistake.

---

## CPM / eCPM / RPM

- **CPM** — price per 1,000 impressions.
- **eCPM** — *effective* CPM: any pricing model normalised to a per-1,000-impressions figure so models can be compared. `eCPM = (revenue / impressions) x 1000`.
- **RPM** — revenue per 1,000 of some publisher-side unit — commonly page views or ad requests, **including** the ones that did not fill. Which unit is in the denominator varies by platform; state it.

**Why I care:** eCPM is the common currency of delivery decisioning. A CPC line item at $0.50 with an expected 1% CTR has an expected eCPM of $5.00 and can be compared against a $5.00 CPM line item.

**Conclusion:** if you cannot normalise CPC and CPA into expected eCPM, you cannot rank demand.

---

## Bid Floor

A minimum acceptable price.

**Why I care:** a floor is a price signal as well as a filter. Without one, buyers can probe downward and discover there is no competition.

**The trade-off:** too high produces no-bids and unsold inventory; too low leaves money unclaimed.

**Soft floors** — a hidden floor placed to capture the gap between the top bid and the second bid — were among the practices that damaged trust in second-price auctions on the sell side.

**Conclusion:** floor optimisation is one of the clearest places an SSP creates measurable value for a publisher.

---

## First-Price Auction and Bid Shading

The winner pays the amount it bid.

**Why I care:** header bidding made a genuinely global second-price auction impractical — each exchange sees only its own demand, so "the second price" has no shared meaning across the whole opportunity. The market moved to first-price.

**The consequence, stated carefully:** in a first-price auction, a buyer that bids its full estimated value captures little or none of the economic surplus from winning — it pays approximately what the impression is worth to it. **Bid shading** is the practice of bidding below estimated value in order to retain surplus, while keeping a sufficient probability of winning. It is an optimisation problem, not a guarantee: shade too much and you lose impressions you wanted.

**Conclusion:** in first-price, price paid and probability of winning are directly coupled. That coupling is what shading models try to navigate.

---

## Win Rate

`wins / bids submitted`, from the buyer's point of view.

**Why I care:** it is a diagnostic that only means something **in context**. There is no universal "good" number.

**Contextual examples:**
* A DSP bidding broadly across open-web display, competing against dozens of buyers on every impression, may run a low single-digit win rate and be perfectly healthy.
* A buyer on a private deal with few competitors may win most of what it bids on, and that is expected rather than alarming.
* A win rate that **changes sharply** after a bidding-model change is the interesting signal — it usually means the change moved price, not that the new number is right or wrong.

**Conclusion:** interpret win rate together with the price paid and the campaign's delivery goals, never as a standalone threshold.

---

## Take Rate

The share of transacted media a platform retains.

**Why I care:** it is the difference between the money flowing through a platform and the money the platform keeps.

**Conclusion:** in this project, `platform_fee = media_amount * take_rate`. How a real company reports revenue in its financial statements depends on principal-versus-agent analysis and varies by company and deal type — see [money-flow.md](money-flow.md).

---

## Pacing

Spreading budget across a campaign's flight.

**Why I care:** a month's budget spent in four hours reaches a narrow slice of the intended audience, usually at poor prices.

**Types:** ASAP, even, front-loaded, and goal-based variants.

**Conclusion:** pacing is a control loop with feedback. It explains why a campaign with a high configured rate can still fail to deliver.

---

## Frequency Cap

A limit on how often a given user sees a campaign within a time window.

**Why I care:** repeated exposure has diminishing and eventually negative returns.

**The trade-off:** a tight cap constrains delivery and can starve pacing; a loose cap wastes budget.

**The technical reality:** a cap requires per-user state, which makes it the most expensive common feature in an ad server — and the one most directly affected by identifier availability. Caps do not carry across browsers, devices, or (without a shared identifier) sites.

**In this project:** frequency capping lives in the **Lab** with synthetic user IDs. Phase 1 production uses no persistent advertising identifier. See [privacy-baseline.md](privacy-baseline.md).

---

## Viewability

MRC display standard: at least 50% of the ad's pixels in the viewport for at least one continuous second. Video has its own thresholds (50% for two continuous seconds), and large formats have separate criteria.

**Why I care:** an impression nobody could have seen has little value. Buying on viewable impressions shifts that risk to the seller.

**Conclusion:** slot placement is an economic decision, not a layout decision.

---

## IVT (Invalid Traffic)

- **GIVT** — General: identifiable through lists and known signatures (declared bots, data-centre traffic, crawlers).
- **SIVT** — Sophisticated: requires analysis to detect (hijacked devices, falsified inventory, automated behaviour designed to look human).

**Why I care:** IVT converts advertiser budget into nothing. A publisher associated with it loses buyer demand for economic reasons, not moral ones.

**Conclusion:** simulate it in the Lab to learn detection. Never generate it against a real monetisation partner.

---

## ads.txt / sellers.json / schain

Three parts of one supply-chain authentication story:

- **ads.txt** — published on the publisher's domain: which advertising systems are authorised to sell this inventory, and in what capacity (`DIRECT` or `RESELLER`).
- **sellers.json** — published by an advertising system: which sellers it represents, and the nature of each relationship.
- **schain** (`source.schain` in OpenRTB 2.6) — carried in the bid request: the ordered list of intermediaries this opportunity actually passed through.

**Why I care:** together they let a buyer verify that the chain in the request corresponds to relationships both parties have declared publicly.

**Conclusion:** this is **authorisation and identity verification, not quality assessment**. A correct ads.txt file says nothing about whether the inventory is worth buying.

---

## SPO (Supply Path Optimization)

A buyer deliberately choosing among the multiple paths that lead to the same inventory.

**Why I care:** the same impression can be reachable through many intermediaries with different fees, latency, and transparency. Choosing better paths increases the share of budget that reaches media.

**In 2026:** SPO is a standard buy-side discipline, and **curation** — sell-side packaging of data and inventory into buyable products — is the corresponding structural shift on the sell side.

---

## Reporting Discrepancy

The gap between two parties' counts of the same activity.

**Sources:** different counting moments (ad served vs rendered vs viewable), ad blockers, abandoned page loads, latency between serving and beacon, timezone and reporting-period boundaries, filtration rules applied by one side and not the other.

**Conclusion:** there is rarely one objectively correct number. There is a **contractually agreed source of truth**. Invoices settle on that agreement, and negotiating it is real commercial work.
