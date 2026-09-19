# Monetisation mechanics — how money actually reaches a bank account

Written because "we serve ads" and "money arrives" are separated by several
steps that nobody explains, and one of them decides whether this works at all.

## The flow

```
1. someone clicks or views an ad on xoxoxo.live
2. the network credits your account BALANCE
3. balance accrues -- you are not paid yet
4. you cross a PAYMENT THRESHOLD          <- this is the step that matters
5. payout runs on their schedule
6. bank transfer arrives, minus FX and bank fees
7. you declare it as income
```

Everyone focuses on step 1. **Step 4 is where small sites die.**

## The thresholds

| | Google AdSense | Amazon Associates |
|---|---|---|
| Threshold | **$100** | **$10** direct deposit · $100 by cheque |
| Schedule | issued 21st–26th monthly, if the balance cleared the threshold by the 20th | 60-day cycle |
| To the bank | up to 7 business days; wire up to 15 | — |
| **Israel catch** | — | Direct deposit requires a **USD/GBP/EUR** account. An ILS account does not qualify — cheque, or a Payoneer account as the usual workaround |

## What that means at our traffic

```
sessions/mo   AdSense @ $2 RPM   months to reach $100   affiliate estimate
     500          $1.00/mo            100 months           $0.12/mo
   2,000          $4.00/mo             25 months           $0.48/mo
   5,000         $10.00/mo             10 months           $1.20/mo
  20,000         $40.00/mo              2 months           $4.80/mo
  50,000        $100.00/mo              1 month           $12.00/mo
```

*(affiliate estimate: 1 impression per session, 0.6% CTR, 2% conversion, ~$2 commission)*

**Today the site has roughly zero sessions.** So the honest answer to "how does
the money reach my bank account" is: at current traffic it does not. Earnings
would sit in a balance indefinitely.

**~20,000 sessions/month is the number that changes the answer.** Below it you
accrue pennies toward a threshold you will not cross. Above it, payouts arrive on
a schedule. That single fact is why the distribution playbook exists and why
more AdTech is not the lever.

## Which path, and why

| Path | Learning | Money | Our ad server |
|---|---|---|---|
| **Affiliate/CPA through our own ad server** | **Highest** | Real, small | ✅ stays in the path |
| AdSense / Ezoic | Low — paste a tag, they decide everything | Higher per visitor | ❌ bypassed |
| Direct sponsor | High — a real contract, a real delivery obligation | Depends on finding one | ✅ |

**Chosen: affiliate through our own ad server.**

Since money is months away whichever path we pick, the right choice maximises
learning per month of waiting. Affiliate keeps our decisioning in the serving
path, so:

- a fixture line item becomes a **real advertiser**
- **Follow One Dollar computes real money** — real payout, real platform fee
- observed CTR and conversion replace configured guesses in `expected_ecpm`
- and we get to reconcile **our** click count against **theirs**, which is the
  reporting-discrepancy lesson with real stakes attached

AdSense pays more per visitor and teaches nothing, because you are not making any
decisions. It becomes interesting later as a *second demand source competing in
our own auction* — which is when Phase 3 floors stop being a simulation.

## Wiring an affiliate offer in

No new infrastructure. A creative already carries a `click_url`, and the click
endpoint resolves the destination server-side from the creative record, so it is
a fixture change:

```json
{
  "id": "li-affiliate", "campaign_id": "camp-affiliate",
  "status": "ACTIVE", "priority": 3,
  "pricing_model": "CPC",  "rate": 0.00,
  "expected_ctr": 0.006,
  "daily_budget": 0,
  "targeting": { "countries": [], "device_types": [], "games": [], "placements": [] },
  "creative_ids": ["cr-affiliate-300x250"]
}
```

```json
{
  "id": "cr-affiliate-300x250", "w": 300, "h": 250,
  "click_url": "<THE AFFILIATE TRACKING LINK>",
  "html": "<!-- a 300x250 creative -->"
}
```

Then `adlabctl seed`. Two notes:

- `rate` is **0.00** until a real commission is observed. Guessing a rate would
  put invented money into the ledger, and the ledger is the one thing that has to
  stay honest.
- `priority 3` puts it below guaranteed demand and above the house fallback.

## Choosing an offer

A casual-games audience is **low intent**. Be realistic: gaming-adjacent offers
convert best — game key stores, peripherals — and general retail converts poorly.
Expect cents.

Programmes that accept a new small site: Amazon Associates (easy, low
commission), and gaming-specific affiliate programmes. Networks that require
traffic volume will decline until Step 3 of the playbook has worked.

## ⚠️ Tax — get proper advice

Receiving foreign business income in Israel involves registration questions
(עוסק פטור / עוסק מורשה), reporting, and VAT that depend on the amounts and on
whether this counts as a business.

**Talk to an accountant before the first payout, not after.** Networks will also
ask for a tax form — typically W-8BEN for a non-US individual receiving
US-source income.

This document is not tax advice and should not be treated as any.
