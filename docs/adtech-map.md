# AdTech Ecosystem Map (as of August 2026)

## 1. The Diagram

```text
                         ADVERTISER  (brand, app, retailer)
                              |  budget
                              v
                          AGENCY / Trading Desk
                              |
                              v
        +-------------------- DSP --------------------+
        |   campaigns, targeting, budget, pacing,     |
        |   bidding logic, frequency across supply    |
        +---------------------------------------------+
             ^         ^          ^            ^
             | OpenRTB | OpenRTB  | OpenRTB    |
             |         |          |            |
        +----+----+ +--+------+ +-+-------+ +--+------+
        |  SSP    | |  SSP    | | Exchange| | Ad      |
        |         | |         | |         | | Network |
        +----+----+ +--+------+ +-+-------+ +--+------+
             ^         ^          ^            ^
             +---------+----------+------------+
                              |
                     Header Bidding / Prebid.js
                              |
                    PUBLISHER AD SERVER  (GAM / Kevel / self-built)
                    "which of my eligible demand sources serves this slot?"
                              |
                          PUBLISHER  (site / app / CTV)
                              |
                            USER  (browser / device)

--- supporting actors (cut across the chain) ---
CMP (consent)          | Verification (viewability, IVT, brand safety)
Measurement / MMP      | Identity providers        | Curation layer
Data providers         | Advertiser Ad Server (buy-side serving + attribution)
```

## 2. Five Distinct Concepts

These are **five distinct functions**. Many companies perform several of them, and the market often uses the labels loosely — but conflating the concepts will make later reasoning wrong.

| Function | The question it answers | Whose interest it serves | Typical economics |
|---|---|---|---|
| **Publisher Ad Server** | *Which of my eligible demand sources serves this slot, and which creative?* | The Publisher's | Tech/serving fee, or self-hosted |
| **SSP** | *How do I represent this seller's inventory to buyers and maximise its yield?* | The Publisher's | Take rate on the seller's revenue |
| **Exchange** | *How do I run a marketplace that matches many buyers to many sellers?* | The marketplace's | Transaction / auction fee |
| **DSP** | *Should this buyer purchase this impression, and at what price?* | The Advertiser's | Fee on advertiser budget |
| **Ad Network** | *I acquire inventory and resell it to advertisers* | Its own | Margin between buy and sell price |

### What actually distinguishes them

**SSP vs Exchange.** An exchange is a *marketplace function* — it runs the matching mechanism. An SSP is a *seller-representation function* — floors, yield rules, inventory packaging, seller identity, reporting for the publisher. In practice most large companies (Magnite, PubMatic, Index Exchange, Google) operate both functions inside one platform, and the industry frequently uses the words interchangeably. **That is a fact about market structure, not about the concepts.** Keeping them separate in your head matters when you ask questions like "who sets the floor?" (SSP function) versus "who determines the clearing price?" (exchange function).

**Ad Server vs Exchange.** The precise statement is:

```text
Ad Server != Exchange
```

An ad server is not a marketplace between independent external buyers. But an ad server **may itself contain auction and yield-optimization logic** — GAM's unified auction and dynamic allocation are exactly that. The ad server's defining job is *delivery decisioning against the publisher's own booked demand and configured demand sources*. The exchange's defining job is *running a marketplace between independent parties*.

**Ad Network vs Exchange.** A network typically takes a **principal** position: it acquires inventory and resells it, and its margin need not be disclosed. An exchange typically acts closer to an **agent**: it intermediates for a disclosed fee. This distinction drives much of the transparency debate and, in real financial statements, principal-vs-agent revenue recognition.

### Two kinds of Ad Server

* **Publisher-side (sell-side)** — manages inventory, line items, delivery, pacing, forecasting. This is what we build in Phase 1.
* **Advertiser-side (buy-side)** — serves creative on behalf of the buyer, counts impressions from the buyer's perspective, supports attribution and cross-channel dedup.

The existence of two independent counting systems is a primary source of **reporting discrepancies**.

## 3. Supporting Actors

| Actor | What it does | Why it matters economically |
|---|---|---|
| **CMP** | Collects consent, produces a TCF/GPP signal | Limited consent constrains what data may be processed, which typically reduces achievable CPM |
| **Verification** | Viewability, IVT, brand safety measurement | Shrinks the inventory that qualifies as billable/quality, and raises the value of what remains |
| **Identity providers** | Stable or probabilistic cross-site identifiers | Materially changes the price a buyer will pay for the same slot |
| **Measurement / MMP** | Conversion attribution | Determines whether the advertiser keeps spending |
| **Curation layer** | Sell-side packaging of data + inventory into a buyable product | A major structural trend of 2025-2026: value migrating toward the sell-side |
| **CDP / DMP** | Audience data management | Third-party DMP usage declined with signal loss; first-party CDP usage grew |

## 4. Where This Project Will Sit

```text
Phase 1-2:  Publisher (ours) + Publisher Ad Server (ours)              REAL
Phase 3:    Auction/selection logic with multiple simulated buyers     REAL code, synthetic demand
Phase 4-5:  Exchange/SSP function + Mini DSP across a network boundary REAL code, synthetic demand
Phase 6:    Real demand (affiliate / direct advertiser)                REAL MONEY
Phase 7:    External Publisher in Shadow Mode                          REAL
Phase 8:    Prebid, schain, sellers.json, video, identity              LEARN / SIMULATE
```

We will deliberately keep the **Publisher**, **Publisher Ad Server**, **SSP/Exchange**, and **DSP** functions as separate components with separate ledgers, even while we own all of them. See [money-flow.md](money-flow.md).

## Sources

- [IAB Tech Lab — OpenRTB](https://iabtechlab.com/standards/openrtb/), [ads.txt](https://iabtechlab.com/ads-txt/), [sellers.json](https://dev.iabtechlab.com/sellers-json/)
- [Google Ad Manager — dynamic allocation and unified pricing](https://support.google.com/admanager/answer/3721872)
- [Equativ — 2026 Guide to Programmatic Curation](https://www.equativ.com/blog/2026-programmatic-curation-guide)
