# How long will this take?

An honest estimate, written 2026-08-28. It separates two things that get
confused, because they have very different answers:

- **Work** — engineering that can be done as fast as we choose to do it.
- **Wait** — calendar time that cannot be compressed by effort, because it
  depends on an audience existing, a partner approving, or a history accruing.

**Work is not the constraint on this project. Wait is.** Almost everything left
on the revenue side is gated on traffic history, and traffic history is the one
thing that cannot be built in a weekend.

---

## The forcing function

**2027-02-08** — AWS Free plan credits expire (~5 months away).

Current run rate is $0.51–2/month. After expiry that becomes real money out of
pocket, but it is *small* real money. The deadline is not "the project dies", it
is "a decision has to be made in January": keep paying ~$5–20/month, or stop.

It matters mainly because **meaningful revenue will not arrive before it does.**
Planning as though it might is the mistake to avoid.

## Track A — AdTech depth (learning)

This is the part that finishes, and it finishes soon-ish. It is almost pure
work with no external gating.

| Phase | Remaining | Estimate |
|---|---|---|
| 3 — yield mechanics | apply the controller, pacing, frequency cap, viewability | 2–3 weeks |
| 4 — mini DSP across a real network boundary | buyer-side pacing, win/loss, reporting discrepancy | 3–4 weeks |
| 5 — OpenRTB 2.6 subset, sellers.json, schain | the real protocol | 3–4 weeks |

**Track A complete: ~2–3 months of part-time work → around November–December 2026.**

At that point the primary goal — *learn AdTech deeply by building and operating
it* — is met. That is worth saying plainly: **the learning finishes long before
the earning.**

## Track B — revenue

Every estimate below is a range because each depends on something outside our
control.

| Step | Work | Wait | Realistic |
|---|---|---|---|
| **B3 affiliate** | ~1 day (the integration is small) | days–2 weeks for approval | **weeks**, once you apply |
| **B4 distribution** | ~2 days (assets, posts, itch.io) | 3–6 months to learn whether anything sticks | **3–6 months to a signal** |
| **B5 programmatic demand** | ~2 weeks | needs 6+ months of traffic history, and most networks reject sites under roughly 10k monthly sessions | **9–15 months, conditional** |
| **Multi-publisher** | ~3–4 weeks | needs a monetisation story worth offering someone else | **after B5, 12–18 months** |
| **B6 mobile app** | 6–8 weeks | gated on an audience existing first | **12–18 months, conditional** |

### What "conditional" means

B5, multi-publisher and mobile are **not scheduled work**. They are work that
becomes possible *if* distribution succeeds. If nobody plays the games, none of
them happen, and no amount of engineering changes that.

This is the uncomfortable finding already recorded in the roadmap: *the blocker
on revenue is not the ad stack, it is that nobody plays the games.* It remains
true and is the single most important sentence in this document.

## The honest totals

| Milestone | When | Confidence |
|---|---|---|
| Track A learning complete | Nov–Dec 2026 | **high** — pure work |
| First real revenue (affiliate) | 2–4 weeks after you apply | **high**, but the amount will be near zero |
| Revenue that covers hosting (~$5–20/mo) | 6–12 months | **medium**, needs distribution to work |
| Revenue worth calling income | 18+ months | **low**, and may never |
| Multi-publisher platform | 12–18 months | **low**, conditional on the above |
| Mobile app | 12–18 months | **low**, conditional |

### Where the money actually is, if it arrives

Ranked by realistic return per unit of effort:

1. **Mobile rewarded video** — $10–30 eCPM against $1–5 for web display. This is
   the only path on the list with a plausible route to real income, and it is
   the most gated.
2. **Affiliate / CPA** — works at low traffic, which nothing else does. The
   right first step for exactly that reason.
3. **Programmatic display** — the thing this project is *about*, and close to
   the worst way to earn from it at this scale. Worth building for the learning;
   not worth waiting for as income.

That inversion is itself one of the more useful things this project has taught.

## What would change these estimates

**Faster:** distribution working better than expected (one game finding an
audience on itch.io or Reddit changes B4 → B5 timing by months); an affiliate
converting at an unusual rate.

**Slower — and more likely:** distribution producing nothing, which is the
default outcome for a new site with no audience and no marketing budget. Plan
for it, and treat any traffic as upside.

## The recommendation

Finish Track A on schedule — it is the primary goal, it is achievable, and it is
entirely within our control.

Treat Track B as an experiment with a real chance of returning nothing. Do B3
and B4 because they are cheap and the learning from *attempting* real
monetisation is genuine. Do not schedule B5, multi-publisher or mobile — let
traffic decide whether they happen.

And make the January decision deliberately: by then Track A will be done, and
the question "is this worth $5–20 a month to keep running?" will have an
evidence-based answer instead of a hopeful one.
