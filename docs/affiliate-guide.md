# Picking and joining an affiliate programme

## The rule that decides everything

**Relevance beats commission rate.** A 10% commission on something our audience
will never buy earns less than 4% on something they might. Our audience is
people who wanted a quick game without installing anything — so the offer has to
make sense in that moment.

## ⚠️ Do not start with Amazon Associates

It is the obvious choice and it is the wrong first move for us:

> Associate accounts that have not referred **three Qualifying Purchases within
> 180 days** of sign-up will be **closed** — with an intermediate deadline of
> **one sale within 90 days**. Closed accounts cannot be reinstated; you have to
> reapply later.

At our current traffic that clock runs out with near-certainty, and it burns the
option. Apply to Amazon **after** the distribution playbook has produced real
visitors, not before.

There is a second Israel-specific problem: Amazon direct deposit is **US and UK
only**. From Israel you would need a Payoneer account (which issues a US
receiving account) or take payment by cheque.

## Shortlist — game key stores

These fit the audience directly, accept international affiliates, generally have
no traffic minimum, and — critically — **no sales deadline that closes your
account**.

| Programme | Commission | Cookie | Where to apply |
|---|---|---|---|
| **Green Man Gaming** | ~5% new customer, 2% returning; up to 10% on bundles | 30 days | <https://www.greenmangaming.com/affiliates/> |
| **Fanatical** | up to 5% | 30 days | search "Fanatical affiliate programme" — runs via a network |
| **Kinguin** ("Kinguin Mafia") | 5–10% per order | 30 days | <https://www.kinguin.net/> → Affiliates |

**Start with Green Man Gaming.** Legitimate storefront, clear public affiliate
page, sensible terms, and a genuinely plausible fit: someone playing browser
games is a person who buys PC games.

Also reasonable, if you would rather promote hardware: **Razer** and **Logitech**
both run affiliate programmes and both accept international publishers.

## What the application will ask, and what to answer

They want to know you are a real site, not a link farm. Ours now is — real
content, a privacy policy, no spam, no scraped text. Answer plainly:

| Field | What to put |
|---|---|
| Website URL | `https://xoxoxo.live` |
| What is your site about | *A small free browser games site — Tic-Tac-Toe, Connect Four and Snake. No sign-up, no downloads, works on mobile.* |
| How do you drive traffic | *Organic, itch.io listings, and gaming communities on Reddit. The site is new and traffic is currently low.* |
| How will you promote us | *A single 300x250 placement below the game on each game page, served by my own ad server. No pop-ups, no interstitials, no auto-refresh.* |
| Monthly visitors | **Be honest.** Say it is new. Inflating it is both pointless and grounds for termination later |

**Do not overstate traffic.** Most programmes accept small sites; almost none
forgive a false declaration.

## Payment setup, from Israel

1. Open a **Payoneer** account — <https://www.payoneer.com>. Israeli company,
   widely used there, and it issues a **US receiving account** so programmes that
   only pay to US/UK banks can pay you.
2. Give the programme those Payoneer details as your bank account.
3. Payoneer transfers to your Israeli bank in ILS, minus an FX spread.
4. Expect the first payout to be **months** away — see
   [monetisation-mechanics.md](monetisation-mechanics.md) for the threshold maths.

## Once you have a tracking link

Send it to me. Wiring it in is a fixture change, not new infrastructure:

```json
{ "id": "cr-affiliate-300x250", "w": 300, "h": 250,
  "click_url": "<YOUR TRACKING LINK>",
  "html": "<!-- 300x250 creative -->" }
```

Then `adlabctl seed`. The line item goes in at `priority 3` — below guaranteed
demand, above the house fallback — with `rate: 0.00` until a real commission is
actually observed, because a guessed rate would put invented money into the
ledger.

## Disclosure — not optional

Most programmes require it, and the FTC and equivalents expect it regardless.
Since the ad is clearly labelled **Advertisement** and sits below the game, we
are most of the way there. When a real affiliate offer goes live, add a line to
the privacy page:

> Some ads on this site are affiliate links. If you buy something after clicking
> one, we may earn a small commission at no extra cost to you.

## What NOT to do

- **No incentivised clicks.** "Click the ad to unlock a level" terminates
  affiliate accounts and violates `CLAUDE.md`
- **No clicking your own links.** Instant, permanent ban everywhere
- **No misleading creatives.** The creative must describe what is actually on
  the other side of the click
- **No stacking programmes** before there is any traffic. One offer, measured
  properly, teaches more than five that each get two clicks
