# Audience

> "Another topic in the roadmap should be audience, which I don't have a
> solution right now."

Neither do I, and this document says so before it says anything else. What
follows is a diagnosis, an honest ranking of the options, and one concrete
recommendation — not a plan that pretends the problem is solved.

**This is the single binding constraint on the entire revenue side of the
project.** Track A finishes regardless. Nothing in Track B happens without this.

---

## The diagnosis, stated plainly

**We built the four most commoditised games on the internet.**

Tic-Tac-Toe, Connect Four, Snake and 2048 are each available in a thousand
places, free, instantly, with no site to visit. For Tic-Tac-Toe and Snake,
**Google serves a playable game directly in the search results** — the user
never leaves the SERP. For 2048 the original is a famous free site with a decade
of backlinks.

This is not a criticism of the build. The games are good, they are fast, they
are above the fold and they work on a phone. But it means:

> **Organic search for our game names is unwinnable, and no amount of SEO work
> changes that.** The competitor is Google's own result, and it is one click
> shorter than us.

Any audience plan that begins with "rank for *play tic tac toe online*" is
already dead. Recognising that now is worth more than three months of learning
it slowly.

## What that leaves

Three strategies. They are not mutually exclusive, but they have very different
odds.

### A. Differentiate the product so there is a reason to come *here*

The only durable answer. Right now there is no sentence that finishes *"I play
at xoxoxo.live instead of Google's version because…"*. Until there is, every
acquisition channel is a leaky bucket.

**The strongest known mechanic for a zero-budget site is a daily puzzle with a
shareable result.** Wordle is the reference case, and the reason it worked is
structural rather than lucky:

- **one puzzle, same for everyone, once a day** — a reason to return tomorrow
- **a spoiler-free shareable result** (the emoji grid) — acquisition, from users,
  at zero cost
- **no account, no install** — nothing between seeing it and playing it

We already have three of the four ingredients: no accounts, instant play, a
records table. What is missing is the *shared daily artefact*.

Concretely, the cheapest version: **a daily 2048 or Snake seed that is identical
for every player, with a result card they can share.** Same board, same food
sequence, same tile spawns — so "I got 2048 in 312 moves" is comparable and
worth posting. That is a day or two of work and it is the highest-leverage thing
left in the project for goal 2.

**Why this and not more games:** a fifth game adds another commodity nobody
searches for us to find. A daily shared puzzle adds a *reason to return and a
reason to tell someone*, which is the actual missing piece.

### B. Go where an audience already is

Stop trying to build a destination; put the games in front of people already
looking for games.

| Channel | Effort | Realistic outcome | Notes |
|---|---|---|---|
| **itch.io** | ~2 hours (packages already built by `tools/build/itch-package.py`) | tens–low hundreds of plays | Browsing audience, tolerant of simple games. The obvious first move |
| **Reddit** — r/WebGames, r/playmygame, r/incremental_games | ~1 hour per post | 0 to a few thousand, high variance | Read each subreddit's self-promo rules first. A ban costs more than the post gains |
| **Game aggregators** — CrazyGames, Poki, GameDistribution | days, plus a quality bar and review | thousands–millions, if accepted | **The only channel with real volume.** They take the monetisation, which conflicts with our ad server — see below |
| **Discord communities** | ongoing | small but sticky | Only works if genuinely participating, not dropping links |
| **Short-form video** | high, ongoing | very high variance | Not a fit unless there is something visually distinctive to clip |

**The aggregator tension is worth naming.** CrazyGames and Poki have the traffic,
and they monetise it themselves — which means our ad server leaves the serving
path and the AdTech learning stops for that traffic. That is a real trade:
*revenue and audience* against *the thing this project is for.*

The resolution: **syndicate a game to an aggregator, keep the site as the
learning environment.** The aggregator becomes a distribution and revenue
experiment; xoxoxo.live stays the place where our own stack runs. They answer
different goals and should not be forced to answer the same one.

### C. Accept low traffic and change the revenue thesis

Entirely respectable, and the most likely outcome. If the site plateaus at tens
of sessions a day:

- programmatic demand is permanently out of reach (networks reject sites below
  roughly 10k monthly sessions)
- affiliate/CPA still works, because it pays per action rather than per
  impression — this is precisely why it is the right first monetisation
- **the learning goal is completely unaffected**

The failure mode to avoid is spending six months on audience work with no signal
because it feels more productive than admitting the thesis did not hold.

## The recommendation

In order, and the order matters:

1. **Build the daily shared puzzle.** ~2 days. It is the only item that changes
   the fundamentals rather than pushing water uphill.
2. **Ship to itch.io.** ~2 hours, packages already exist, no downside.
3. **Post to two subreddits**, having read their rules. ~1 hour.
4. **Then wait four weeks and look at the numbers**, rather than adding channels.
5. **If there is any signal**, apply to one aggregator. If there is none, go to
   strategy C and stop spending time here.

Doing 2 and 3 before 1 is the tempting order and the wrong one: sending traffic
to a site with no reason to return converts a scarce, one-time opportunity into
nothing.

## What to measure, and the honest thresholds

Set before starting, so the answer is not negotiated afterwards:

| Metric | Why | Threshold to continue |
|---|---|---|
| **Returning sessions (7-day)** | the only real signal that the product works | > 15% |
| **`game_replay` rate** | do they play twice in one visit | > 35% |
| **Shares per daily-puzzle completion** | the acquisition engine, if it exists | > 3% |
| **Sessions per week** | the input everything else needs | trending up at all |

If after **six weeks** returning sessions are under 10% and weekly sessions are
flat, the honest conclusion is that this audience does not exist for this
product, and Track B should be scaled back to affiliate-only. Writing that
threshold down now is the point — it is much harder to be honest about it later.

## Why there is no better answer

Worth stating so this is not revisited hopefully every month:

- **No budget.** Paid acquisition would need TAC below revenue, and revenue is
  approximately zero. Buying traffic to manufacture numbers is also explicitly
  forbidden in `CLAUDE.md`.
- **No existing audience.** No mailing list, no following, no community.
- **A commodity product** in the most competitive free-content category on the
  internet, against Google itself.
- **No network effect** — the games are single-player or same-device, so playing
  does not recruit anyone.

Item four is the one the daily-shared-puzzle idea attacks directly, and it is
the only one of the four we can change without money. That is why it is the
recommendation.


---

## How "returning" is actually measured

This threshold sat in this document for weeks as a number nothing could
produce. Session identity lives in `sessionStorage`, deliberately, so the same
person across two days was two strangers.

It is measured now, and the method matters as much as the number.

The browser keeps **one date** in `localStorage` — the last day it visited.
That date is never transmitted. What goes into `session_start` is a boolean and
one of five constants:

| bucket | meaning | counts as returning (7d) |
|---|---|---|
| `new` | no previous visit | no |
| `d0` | earlier the same day | **yes** |
| `d1_7` | one to seven days ago | **yes** |
| `d8_30` | eight to thirty days ago | no |
| `d30_plus` | over a month ago | no |
| `unknown` | storage blocked | excluded entirely |

Three properties, each of which is a decision rather than an accident:

**It is not an identifier.** Every returning browser sends the same five-value
string. There is nothing to join on, nothing that survives a cleared cache into
a profile, and nothing that says anything about a person beyond the fact that
this browser has been here. `CLAUDE.md` gates the introduction of a persistent
advertising identifier; this is deliberately not one, and the privacy posture is
unchanged.

**It answers the question asked, not a larger one.** We want to know whether
people come back within a week. The client computes exactly that and sends the
answer. Sending the date instead would let us compute more later — which is
precisely the argument that ends with a profile.

**A blocked browser is excluded, not counted as a loss.** Reporting those
sessions as "did not return" would understate the rate by exactly the count of
privacy-conscious players, and they are not a random sample of the audience.
