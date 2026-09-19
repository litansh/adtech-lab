# Backlog

Everything discussed, in one place, with what blocks each item. Updated
2026-08-29.

The organising question is **what is waiting on whom** — most of what remains is
not engineering.

---

## Blocked on Litan

Nothing below can move without you. Ordered by how much it unblocks.

| # | Item | Effort | Unblocks |
|---|---|---|---|
| ~~1~~ | ~~deploy the ad server~~ | — | **Done 2026-08-29.** Verified live: filtering refuses headless/Googlebot/GPTBot/curl, the cap holds at 3 then falls through to a real auction, and the supply chain verifies over the network |
| 2 | **Affiliate signup** | ~1 hour | The only revenue path that works at low traffic. See `affiliate-guide.md` |
| 3 | **itch.io account + 6 project pages** | ~1 hour | Six verified ZIPs and paste-ready copy are waiting in `dist/itch/` |
| 4 | **Reddit posts** (r/WebGames, r/playmygame) | ~1 hour | Read each subreddit's self-promo rules first; a ban costs more than the post gains |
| 5 | **Confirm the SNS subscription email** | 1 min | Budget and anomaly alerts currently reach nobody |
| 6 | **Decide on the budget freeze** (`infrastructure/guardrails/freeze.tf`) | 5 min | Written, never applied |

**Item 1 is the one that matters most today.** Everything else is business; that
one is a deploy of work already built and tested.

## Ready to build, not blocked

In the order I would take them.

| Item | Why now | Notes |
|---|---|---|
| ~~Surface the delivery projection~~ | ~~built~~ | Lab-only `/delivery` endpoint |
| ~~Levels for 2048~~ | ~~built~~ | Tile milestones, keyed on the minimum score to build each |
| ~~Shadow mode~~ | ~~built~~ | Records nightly; needs 14 days of traffic before `evaluate` says anything |
| **Levels for 2048** | The mechanic worked on Snake; tile milestones are the natural fit | Bar keyed on the minimum score for the next milestone |
| **Fire the Cost Fuse from an alarm** | It has never fired from an alarm, only by hand. Every figure in `cost-model.md` is an estimate | The oldest open item in the project |
| **Daily for a second game** | One daily is a habit; two is a reason to stay | 2048 daily seeded the same way |
| **`app-ads.txt`** | Cheap, and required the day a mobile app exists | Blocked on nothing, useful only later |

## Conditional — do not schedule

These become possible *if* distribution works. Building them first repeats the
mistake ADR 0003 exists to prevent.

| Item | Condition |
|---|---|
| **Programmatic demand (B5)** | ~10k monthly sessions and 3–6 months of history. Networks reject sites below that |
| **Multi-publisher** | A monetisation story worth offering someone else — i.e. after B5 |
| **Mobile app (B6)** | A real audience. Rewarded video at $10–30 eCPM is the best revenue path on the list and the most gated |
| **The remaining agents** | Traffic worth optimising. Five agents on today's volume would fight over noise |

## The agent fleet, in build order

From `agent-fleet.md`. The order is the arbitration order **in reverse** —
build the brakes before the accelerator.

| Agent | Status |
|---|---|
| **Yield** | built (`yieldctl`), proposal-only |
| **Integrity** | rules built (`integrityctl`); no job consumes them |
| **FinOps** | built (`finopsctl`), never run on real data |
| **Demand** | built (`demandctl`) |
| **Product** | built (`productctl`) |
| **Feedback** | built (`feedbackctl`), capped at Recommend permanently |
| **Reliability** | exists as alarms, not as an agent. Needs CloudWatch |
| **Horizon** | not built. Needs the open web; capped at Recommend permanently |

## Honest gaps

Recorded so they are not mistaken for finished work.

- **Nothing has run on real traffic.** Every number in this repo is from the Lab
  or from synthetic events. The nightly report correctly says "no data".
- **Classifier precision and recall are unmeasured.** There is no ground truth,
  so the thresholds in `filter.go` are thresholds, not accuracies.
- **No agent has run in shadow mode**, so no agent has evidence it would have
  helped.
- **The Cost Fuse has never fired from an alarm.**
- **Field-experiment interactions are not measured** — one field at a time, with
  2ⁿ combinations and traffic for a handful of arms.

## Done

For scale, not for celebration.

**Track A, Phases 1–5.** Publisher and ad server; the Lab and a first-price
auction; yield mechanics (floors, frequency capping, pacing, viewability) with a
cold-path controller; a mini-DSP across a real network boundary with the
reporting discrepancy measured at 19.1%; OpenRTB 2.6 subset, `ads.txt`,
`sellers.json` and `schain` with a verifier that walks the chain like a buyer.

**Track B.** Six games plus the Daily Puzzle; a PWA; a records table; levels;
two new ad placements chosen against a written policy; six verified itch.io
packages with paste-ready listings.

**Platform.** 285 browser assertions across three viewports; CI on every PR;
deploys via GitHub OIDC with no stored credentials; a nightly cold-path job that
commits its own report.
