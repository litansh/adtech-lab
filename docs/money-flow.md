# One Impression Journey and One Dollar Journey

## Part A — One Impression

Scenario: a user opens an article on `sport-news.example`. There is one `300x250` slot in the right rail, above the fold. The publisher runs **client-side Prebid.js with GPT/GAM** — the most common open-web setup. Other integration models exist (server-side Prebid Server, GAM-only, SSP-direct tags, in-app SDK mediation); the ordering below is specific to this one.

```text
T+0ms     Page HTML arrives. GPT and Prebid.js load. Ad slot defined, not yet requested.

T+20ms    pbjs.requestBids() starts the Prebid auction.  Prebid timeout = 1000ms.
          GPT is instructed to wait for Prebid before sending its ad request.

T+25ms    6 SSP/exchange adapters fire in parallel from the browser.

T+30ms    SSP #2 receives the request and builds an OpenRTB BidRequest.
          Its own buyer deadline: tmax = 250ms (so it can answer Prebid in time).
          {
            "id": "a7f3-9c21",                       <- auction id (this SSP's auction)
            "imp": [{ "id": "1", "banner": {"w":300,"h":250},
                      "bidfloor": 1.50, "bidfloorcur": "USD",
                      "tagid": "sidebar_1" }],
            "site":   { "domain": "sport-news.example",
                        "publisher": {"id":"pub-88"} },
            "device": { "ua": "...", "geo": {"country":"IL"}, "devicetype": 2 },
            "user":   { "eids": [ ... ] },
            "regs":   { "gpp": "DBABMA~...", "gpp_sid": [2] },
            "source": { "schain": { "complete":1, "ver":"1.0",
                        "nodes":[{"asi":"ssp2.example","sid":"pub-88","hp":1}] } },
            "tmax": 250,
            "at": 1                                   <- first price
          }
          Fanned out to 40 DSP endpoints in parallel.

T+38ms    DSP C  -> HTTP 204 no-bid (no eligible campaign for this geo)
T+45ms    DSP A  -> bid $1.20 CPM   (below the $1.50 floor -> will be rejected)
T+52ms    DSP B  -> bid $2.80 CPM   (user matched a retargeting segment)
          ... 25 more DSPs respond with no-bid or low bids ...

T+280ms   SSP #2 hits its own tmax deadline (T+30 + 250ms).
          12 DSPs never responded. They are abandoned, not waited for.

T+285ms   SSP #2 runs its auction over the responses it actually received:
             DSP B $2.80  -> winner. First price: clearing price = $2.80.
             DSP A $1.20  -> rejected, below floor.
          SSP #2 applies its take rate and returns $2.38 CPM to Prebid.

T+320ms   All 6 adapters have answered (or timed out). Prebid auction closes,
          well inside its 1000ms timeout.
             SSP#2 $2.38 | SSP#5 $2.10 | SSP#1 $1.90 | 3 no-bids

T+325ms   Prebid sets targeting key-values on the GPT slot:
             hb_pb=2.30  hb_bidder=ssp2  hb_adid=...
          (hb_pb is the price bucket, not necessarily the exact bid)

T+330ms   GPT sends the ad request to GAM, carrying those key-values.

T+360ms   GAM performs final ad selection. This is where the publisher's own
          booked demand is considered — after Prebid, not before:
             - direct-sold guaranteed line items, by priority and delivery need
             - the header-bidding price-priority line item matched by hb_pb ($2.30)
             - AdX/AdSense real-time demand via dynamic allocation ($2.20)
          Today the guaranteed "Bank Leumi" line item at $12.00 CPM is already
          fully delivered for the day, so it does not compete.
          -> header bidding line item wins.

T+380ms   GAM returns the winning creative.
T+420ms   Creative renders inside a SafeFrame.
T+430ms   Impression counted (GAM's count; Prebid and the DSP each count their own).
T+1.6s    The ad was >=50% in view for 1 continuous second -> Viewable Impression (MRC display).
T+8s      User clicks.
T+3d      User converts. Attributed under a 7-day post-click window.
```

### What the numbers teach

* **Low bid density is normal.** 40 DSPs were asked; a handful returned a usable bid. Every timeout still consumed the SSP's compute and connections for nothing. That is the pressure behind **traffic shaping**.
* **The deadline is the contract.** The 12 DSPs that missed `tmax` did not "lose the auction" — they were never in it. Latency is not a performance nicety here; it is eligibility.
* **Three parties counted this impression** (GAM, Prebid, DSP B), at three different moments. This is where discrepancies are born.

---

## Part B — One Dollar

> ### Illustrative commercial model
> **Not a universal programmatic fee structure.** Agency, DSP, data, verification and SSP charges are structured very differently across deals — flat fees, CPM fees, percentage-of-spend, net-media buys, principal-based buying. The point of this table is to show *where fees attach*, not to claim these are the market rates.

Follow **$10.00** of advertiser gross spend, on a CPM basis (per 1,000 impressions).

```text
$10.00   Advertiser gross spend (what the brand books)
 -$0.50  Agency / trading desk fee (illustrative 5%)
========
 $9.50   reaches the DSP
 -$1.43  DSP fee (illustrative 15%)
 -$0.30  Data / audience segment fee
 -$0.10  Verification (pre-bid + measurement)
========
 $7.67   the amount the DSP actually bids into the marketplace
 -$1.15  SSP/exchange fee (illustrative 15%)
========
 $6.52   Publisher gross revenue
```

### What the industry studies actually say

Be precise here — these numbers are widely misquoted, including by me in the first draft of this document.

* **ISBA/PwC (2020, UK)**: for every £1 of advertiser spend, on average about **51p reached the publisher**, with roughly 15% an unattributable "unknown delta".
* **ANA/PwC/TAG TrustNet (2023, US)**: approximately **71% of advertiser spend reached seller/publisher revenue** after supply-chain transaction costs. A further ~35% of the total was then lost to **media quality problems** — non-viewable, unmeasurable, fraudulent, or MFA inventory — leaving approximately **36% as "TrueAdSpend"**.

> **`TrueAdSpend` ≈ 36% is not the amount paid to publishers.** It is the share of the advertiser's budget that ended up as impressions meeting quality criteria (fraud-free, measurable, viewable, non-MFA). Publishers received roughly 71%; the gap is waste, not fees. Later ANA benchmarks report TrueAdSpend improving (about 39% by Q3 2025).

The learning point is that there are **two separate leaks**: fee take-out along the chain, and quality loss at the destination. They require completely different fixes.

### Where each term appears

| Term | In the model above | Notes |
|---|---|---|
| **Advertiser Spend** | $10.00 | An expense, never anyone's revenue |
| **Platform fee / take rate** | $1.43 (DSP) + $1.15 (SSP) | What the intermediaries retain |
| **Media handled (gross)** | $7.67 through the SSP | Flows through, mostly owed onward |
| **Publisher gross revenue** | $6.52 | Before the publisher's own costs |
| **Media waste** | not in this table | The ANA quality loss — money spent on impressions that did not qualify |

---

## Part C — Separate Economic Roles and Ledgers

We own the whole experiment, but we will **model four separate economic entities** with **separate virtual ledgers**. Collapsing them is the fastest way to learn AdTech economics incorrectly.

```text
ledger:advertiser   gross_spend, working_media, fees_paid
ledger:dsp          media_purchased, dsp_fee_retained, infra_cost
ledger:ssp          gross_media_handled, publisher_payout, platform_fee_retained, infra_cost
ledger:publisher    publisher_revenue, traffic_acquisition_cost, contribution_profit
```

### Publisher economics (our O&O mini-games site)

```text
publisher_revenue              what our ad server credits the publisher entity
- traffic_acquisition_cost     paid traffic, content cost, hosting attributable to the site
= publisher_contribution_profit
```

### Platform economics (our ad server / SSP function)

```text
gross_media_handled            total value transacted through the platform
- publisher_payout             owed onward to the publisher entity
= platform_fee                 what the platform retains
- infrastructure_cost          AWS
- vendor_cost                  third parties, when any exist
= platform_contribution_profit
```

**These are not the same business.** A publisher can be profitable while the platform serving it loses money, and vice versa. Keeping the ledgers separate is what makes the "Follow One Dollar" feature meaningful.

Because the Publisher is a real product with sessions rather than a page with a slot on it, the Publisher ledger gains a second denominator worth tracking from Phase 1:

```text
revenue_per_session   = publisher_revenue / sessions
publisher_RPM         = publisher_revenue / <stated denominator> x 1000
```

`revenue_per_session` is the metric that makes the monetisation-versus-experience trade-off visible. Raising ad frequency raises impressions per session, but if it shortens sessions, `revenue_per_session` can fall while impressions rise. See [publisher-product.md](publisher-product.md).

### A note on accounting terminology

For this project we define one metric explicitly:

```text
platform_fee = media_amount * take_rate
```

Real companies' **financial-statement revenue** is a different question. Whether a company reports gross media handled or only its retained fee depends on **principal-versus-agent** analysis under the applicable accounting standards, and it varies by company and by deal type — an SSP acting as principal on a resale may book gross, while the same company acting as agent books net. We are not going to claim a single universal rule. We will simply be explicit about which number we mean, every time.

---

## Follow One Dollar — the feature (Phase 2)

One query answering: *"Where did this money go?"*

```text
auction_id: a7f3-9c21
  advertiser_gross_spend       $0.00280   (value_cpm $2.80 / 1000)
  publisher_payout             $0.00238   (85%)
  platform_fee                 $0.00042   (15% take rate)
  attributed_infra_cost        $0.0000034
  platform_contribution_profit $0.00042 - $0.0000034
```

**The economic shape of AdTech:** the marginal infrastructure cost of an impression is on the order of micro-dollars, while the marginal fee is on the order of milli-dollars. That is why the business is driven by volume and take rate rather than by infrastructure efficiency — **as long as a high share of requests monetise**. When fill or win rates are low, you pay compute on requests that earn nothing, and the ratio degrades quickly. That is precisely where infrastructure economics and marketplace economics meet.

## Sources

- [ANA Programmatic Media Supply Chain Transparency Study (2023)](https://www.ana.net/content/show/id/pr-2023-06-programmaticstudy) · [TAG TrustNet / Fiducia summary of the ANA study](https://www.fiducia.eco/anastudy1) · [ANA Q3 2025 Programmatic Transparency Benchmark](https://www.ana.net/content/show/id/pr-2025-11-transparency)
- [Prebid — how Prebid.js works with GPT/GAM](https://docs.prebid.org/overview/intro.html)
- [Google Ad Manager — dynamic allocation](https://support.google.com/admanager/answer/3721872)
