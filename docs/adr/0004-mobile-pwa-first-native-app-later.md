# ADR 0004 — PWA first, native app as a later, gated phase

**Status:** accepted · 2026-08-28

## Context

A mobile app is proposed for the roadmap. "Mobile app" turns out to mean two
very different projects with opposite cost/benefit, so they are separated here.

### What a native app would genuinely teach

In-app advertising is not web advertising with a smaller screen. It is a
different domain, and one we currently classify as NOT RELEVANT:

| Concept | Why it is different |
|---|---|
| `app-ads.txt` | Authorisation for app inventory. Currently marked NOT RELEVANT because we have no app |
| **Mediation waterfalls vs in-app bidding** | The mobile equivalent of the header-bidding transition, and it happened later and differently |
| SDK rendering | No iframe, no `SafeFrame`. The ad renders inside the app process |
| **IDFA / GAID and ATT** | A completely different identity model, with an OS-level consent prompt |
| **SKAdNetwork / Privacy Sandbox for Android** | Attribution *without* user-level data. Genuinely novel, and nothing on web resembles it |
| MMPs (AppsFlyer, Adjust) | An entire measurement layer with no web equivalent |
| **Rewarded video** | The dominant monetisation model for mobile games |

That last row also matters for goal 2. Rewarded video on a mobile game commonly
earns **$10-30 eCPM** against **$1-5** for open-web display. If real revenue is
the objective, this is a materially better path than banner ads on a web page.

### What it would cost

```
Apple Developer Program     $99/year         (~$8/month against a $100 ceiling)
Google Play                 $25 one-time
Build toolchain, store review, release cycles, two platforms
```

And the schedule constraint: the AWS Free plan expires **2027-02-08**. A native
app plus an ad SDK integration does not fit in five months alongside Track A.

### The mistake we would be repeating

We just corrected an ordering error in ADR 0003: building yield mechanics when
there was one buyer meant building machinery we could not observe working.

**Building a mobile app before anyone plays the web games is the same mistake.**
An app store listing with no audience produces no installs, and an ad SDK with
no installs teaches nothing that reading the documentation would not.

## Decision

**Two steps, deliberately far apart.**

**Now — a PWA.** Make the existing site installable: a web app manifest, icons,
and a service worker for offline play. The games are already client-side, so
offline is nearly free.

- Cost: **$0**. No store account, no review, no second codebase.
- Buys: home-screen presence and offline play, which is real retention — and
  retention is Track B's actual blocker.
- Teaches about AdTech: **almost nothing.** It is still web inventory. That is
  fine; it is a product step, not a learning step, and it is honest about that.

**Later — a native app, as a gated phase (B6).** Prerequisites, all of them:

1. The web games have a real, returning audience
2. Track A has reached Phase 5, so the web-side concepts are actually learned
3. An explicit decision on the AWS account plan, since this crosses February
4. Acceptance of the $99/year and the store review cycle

At that point it becomes the gateway to in-app AdTech: `app-ads.txt`, mediation
versus in-app bidding, ATT, SKAdNetwork, rewarded video. That is a legitimate
Phase 8 sitting behind a real audience.

## Alternatives

- **Native app now.** Rejected: no audience, no time before February, and it
  repeats the ADR 0003 error.
- **A wrapper (Capacitor/React Native) shipping the existing web games.** Cheaper
  than native, and tempting. Rejected *for now* for the same reason -- but this
  is the right shape when B6 arrives, because it reuses the games rather than
  rewriting them.
- **PWA and nothing else, ever.** Rejected: it forecloses the in-app AdTech
  domain, which is a large and genuinely different part of the industry.

## Cost impact

PWA: $0. Native app: $99/year plus $25, and a schedule cost measured in months.

## When to reconsider

When the web games have returning players. That single fact unlocks both the
revenue case and the learning case; without it neither exists.
