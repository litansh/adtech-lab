# Where ads may go

> "Need to find more areas for ads without harming the games."

The constraint in `CLAUDE.md` is the starting point, not an obstacle:

> Never create an ad opportunity merely because we technically can; every
> monetisation change is measured against product metrics in the same
> experiment.

So the question is not *where can a slot fit* — slots fit anywhere. It is
**where does a slot compete with playing, and where does it not.**

---

## The test for a placement

A placement is acceptable when all four hold:

1. **It does not push the board.** Anything above the board costs the thing we
   spent the most effort protecting — the board being visible without
   scrolling.
2. **It does not delay an action the player has already decided to take.**
   Especially "play again", which is the metric that tells us whether the
   product works at all.
3. **It does not move after load.** A slot that fills and reflows the page is
   worse than no slot: layout shift during play is indistinguishable from a bug.
4. **It is measured against `game_replay` in the same experiment.** A placement
   that raises revenue and lowers replay rate has borrowed tomorrow's sessions
   to pay for today.

Rule 4 is the one that makes the other three enforceable rather than
aspirational.

## The moments where a player is genuinely between activities

This is the whole idea. An ad is intrusive when it interrupts; it is ordinary
when it occupies attention that is not currently spent on anything.

| Moment | Competing with play? | Verdict |
|---|---|---|
| **Home page, browsing games** | no — nobody is playing | **safe** |
| **After `game_end`, before replay** | no — the game is over | **safe if it does not delay replay** |
| Below the board, during play | marginal — it is out of the way | current slot |
| Above the board | **yes** — pushes the game down | never |
| Overlay during play | **yes** | never |
| Interstitial before replay | **yes** — delays a decided action | never |

## What we added

**1. Home page slot.** The home page is a browsing surface, not a playing
surface. A slot among the game cards competes with nothing, and it is the first
page distribution traffic lands on — so it is also the slot most likely to be
seen.

**2. End-of-game slot.** Appears only after `game_end`, below the actions,
never before and never above the replay button. The replay button does not
move when it fills, because the slot is appended below it rather than inserted
above it.

Both are lazily requested, like the in-game slot: no ad is fetched until the
slot is near the viewport. An unseen impression still fans out to buyers, and
buyer calls are 37.8% of infrastructure cost per thousand requests.

## What we deliberately did not add

- **A second in-game slot.** There is room on a laptop. There is no *attention*
  spare, and a second slot on a page with one game halves the value of the
  first without adding a second buyer.
- **A sticky/anchored unit.** Reliably viewable and reliably resented. It would
  raise measured viewability while lowering the thing viewability is a proxy
  for.
- **An interstitial before replay.** The single highest-revenue placement in
  mobile gaming and a direct tax on the only engagement metric we have.

Writing down what was rejected matters more than the list of what was added: a
year from now, "why don't we have an interstitial?" should have an answer better
than nobody thought of it.

## How a new placement gets approved

1. Ship it behind a client-side bucket, logged per session.
2. Run it for a fixed period decided **in advance**.
3. Compare on **revenue per session** *and* **`game_replay` rate**.
4. It ships permanently only if replay rate is not worse. Not "not
   significantly worse" — the burden is on the placement.

That asymmetry is deliberate. Revenue is easy to measure and immediate; the
damage a bad placement does is slow and shows up as traffic that never returns,
which no experiment of reasonable length will detect. When the two signals are
close, the product wins.

**This is now enforced by code rather than by intention.** `productctl placement`
reads the arm from the event log and returns one of four verdicts. Run against
1400 sessions where the placement earned 57.6% more revenue and cost 20.6% of
replay rate, it returns REMOVE — and it would return `DO NOT SHIP` on the same
revenue gain with an *inconclusive* replay result, because inconclusive is not a
pass:

> With revenue improving and engagement unmeasurable, shipping is a bet that the
> invisible cost is zero. That bet is how sites decay.
