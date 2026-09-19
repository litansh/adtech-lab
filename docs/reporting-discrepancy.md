# Reporting discrepancy

Two companies count the same events and get different numbers. Every media
contract has a clause about it, every QBR has a slide about it, and it is
permanent — not a bug anyone is going to fix.

Now measured, on our own two services.

---

## The measurement

300 requests, our ad server against `services/mini-dsp` over real HTTP:

| | Seller (SSP) | Buyer (DSP) |
|---|---|---|
| bids submitted | — | 300 |
| **wins** | **251** | **203** |
| **spend** | **$2.4180** | **$1.9516** |

**The buyer under-counts by 19.1%.** Neither side is wrong. Neither side is
lying. There is no reconciliation that makes these agree, because they are
answers to different questions asked at different points in a pipeline.

## Where the gap comes from

Ranked by how much they matter in practice.

### 1. Lost win notices — the dominant cause

The seller tells the buyer it won by calling the bid's `nurl`. That call is
fire-and-forget: a GET issued while the page is already moving on, or from a
server that has finished the request. It can be dropped by a network, a
timeout, an ad blocker, or a browser tearing down the page.

**Everything the buyer believes about delivery flows through that call.** When
it is lost, the buyer never learns it won — permanently, silently, with no
error on either side. Our mini-DSP drops 15% of notices deliberately, and that
single mechanism produces almost the whole 19% gap.

This is not a simulation artefact. It is why buy-side numbers are
systematically *lower* than sell-side numbers across the industry.

### 2. The impression that never rendered

The seller counts a decision; the buyer wants to count a view. Between them the
user can leave, the creative can fail, a blocker can intervene, the tab can be
closed. Sellers who count at decision time and buyers who count at render time
are measuring different populations by design.

### 3. Timeouts, counted by one side only

A buyer that answers after `tmax` did the work and was never in the auction.
The buyer may count it as a bid; the seller never saw it. Our `buyer-slow`
times out on 75% of calls and wins nothing — from its own logs it looks busy.

### 4. Filtering applied at different points

Each side removes invalid traffic on its own terms and at its own moment. Ours
is in `filter.go` pre-bid plus `integrityctl` retroactively; theirs is whatever
their vendor does. Two independent filters never agree, and the retroactive one
means **the same day's number changes after the fact**.

### 5. Time zones and day boundaries

We aggregate on UTC days. A buyer on US Eastern has a day boundary five hours
away, so "yesterday" is a different set of events entirely. This one is trivial
and constant, and it is the first thing to rule out.

### 6. Currency, rounding and fees

Rounding per impression at $0.004 is noise; rounded across ten million it is a
line item. And gross-versus-net is a definitional gap, not a counting one — the
buyer's spend includes fees the seller never sees as revenue.

## What is actually done about it

Not "fixed" — **bounded and agreed**.

- **A tolerance in the contract.** Commonly 5–10%. Above it, the sell side
  bills on the buy side's numbers, which is why sellers care about the gap even
  when they are the ones counting higher.
- **Sell-side numbers are the billing record**, by convention, because the
  seller is the one who served.
- **A shared event id** on both sides makes the gap *decomposable* rather than
  merely visible. Without one you can argue about the total and never about the
  cause.

That last point is the practical lesson. Our bid ids flow through the `nurl`, so
we could in principle join both logs and say exactly which 48 impressions the
buyer never learned about — which converts an argument into a query.

## Two bugs this exercise found

Both were invisible until two processes had to agree, which is the entire
argument for building the buyer as a separate service.

**A relative `nurl` is silently useless.** The DSP first returned
`/win?bid=...`. The seller cannot resolve a relative URL, so the call never
happened, and the only symptom was a **100%** discrepancy with no error
anywhere. Win notices must be absolute.

**A network buyer's creative is not in the seller's control plane.** The ad
server looked up `alloc.CreativeID` locally, found nothing, and returned
`no_ad` — turning *every* win by the real DSP into a blank slot. A buyer
supplies its own markup in `adm`; that is what `adm` is for. The bug produced no
error and no trace, and looked exactly like weak demand.

## Running it

```
make dsp        # start the buyer on :8081
make ad-server  # start the seller on :8090
make discrepancy
```

`GET http://127.0.0.1:8081/stats` is the buyer's own view, and it says in its
own response body that it will not match ours.
