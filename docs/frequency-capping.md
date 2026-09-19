# Frequency capping

The clearest case in this project where privacy and advertising effectiveness
**genuinely** trade off.

Most privacy conflicts in AdTech dissolve under examination — it turns out you
did not need the identifier, you needed the aggregate. This one does not
dissolve, because a frequency cap is *defined over a person*:

> Show this to **one person** no more than **N times** in **a period**.

You cannot implement that definition without knowing that two requests came from
the same person, which is exactly the capability our privacy baseline declines
to build.

---

## What we built, and what it cannot do

**Session-scoped capping.** The cap holds within one browsing session, keyed on
a `session_id` that already exists, lives for the tab, and identifies nobody
across sessions, devices or days.

**What that buys — most of the value.** "Do not show the same banner five times
while somebody plays three games" is the common complaint frequency capping
exists to solve, and it needs no persistent identifier at all.

**What it cannot do**, stated plainly rather than glossed:

- cap across sessions, days or devices
- measure **true** frequency — we can only report *per-session* frequency, and
  presenting that as if it were per-person would be a lie
- resist a client that discards its session id to reset the cap

The third deserves dwelling on. A session id is **client-asserted**, so a
determined client can reset its own cap. That is acceptable while the advertiser
is us. It stops being acceptable the moment someone *pays* for a capped
campaign — and at that point the honest options are a real identifier (a gated
decision, per `CLAUDE.md`) or **not selling capped campaigns**. Not "tighten the
heuristics".

## Decision-time or impression-time?

We count at **decision** time. The trade:

| | Counts | Fails by |
|---|---|---|
| **Decision-time** (ours) | what we *served* | over-counting — a served ad that never renders still counts |
| Impression-time | what was *seen* | under-counting during the gap between deciding and the pixel firing, so a burst in one session can all pass a cap that should have stopped the second |

Impression-time is more truthful and would need the session id inside the signed
token — a token format change.

Decision-time errs towards **respecting** the cap, which is the right direction
to be wrong in: under-delivering a capped line item is a delivery problem,
over-delivering it is a broken promise to the advertiser.

## Design decisions worth stating

**A cap that cannot measure does not fire.** No session id means no counting, so
no cap applies and the ad serves. A cap that fires when it cannot measure is a
cap that silently kills delivery.

**An unimplemented scope serves uncapped.** `scope: "day"` is not implemented,
and it does *not* silently fall back to session semantics — it serves uncapped.
Adding "day" later must be a visible decision requiring an identifier, not a
config value somebody sets without noticing what it implies.

**The Trace carries the numbers.** A capped candidate reports
`frequency_capped` with `frequency_seen` and `frequency_cap`, not a category.
"Why did this not serve?" should be answerable with a number.

**The house fallback is never capped.** A capped fallback means serving nothing,
which is worse than serving a house ad.

## Why capping exists at all — the reach/frequency trade

Worth stating, because the mechanic makes no sense without it.

A fixed budget can buy **many impressions to few people** or **few impressions
to many people**. Frequency capping pushes toward the second:

- **Diminishing returns.** The fifth impression to one person is worth far less
  than the first impression to a fifth person.
- **Negative returns.** Past a point, repetition produces irritation, and
  irritation attaches to the brand.
- **Reach is usually the objective.** Most brand campaigns are buying *unique
  people*, and uncapped delivery quietly converts a reach campaign into a
  frequency campaign.

**The cost:** a capped line item is harder to deliver. It must find new people
rather than re-showing to available ones, which interacts directly with pacing —
a cap can make a budget undeliverable, and the ad server will not say so unless
someone measures. That interaction is the next thing to build.

## The storage design is the whole cost question

A session-scoped counter has **no value once the session ends**, so rows expire
rather than accumulate. That is a cost property and a data-minimisation property
at once — two independent reasons pointing the same way, which is usually a sign
the design is right.

In AWS this is DynamoDB with a short TTL, following `DynamoBudget`: an atomic
counter plus a per-invocation cache. Locally it is a map with the same TTL
semantics, so the behaviour under expiry is tested rather than assumed.

## What is still missing

- **Cap scopes beyond the session** — requires an identifier, and therefore the
  gate.
- **Capping by campaign or advertiser**, not just line item. The store already
  takes an arbitrary subject, so this is configuration rather than code.
- **The pacing interaction.** A cap makes delivery harder and nothing currently
  reports "this line item cannot deliver its budget under its cap."
- **Measured effect.** We have not measured what capping does to revenue or to
  `game_replay`. Until we do, the cap is a correctness feature, not a yield one.
