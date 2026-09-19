# The Orchestrator agent

The other seven agents each answer a question about the system. This one
answers a question about **the person running it**: given where the plan
actually is, what the fleet is waiting on, and what the funnel shows — what is
the single next thing worth doing?

```
go -C tools/orchestratorctl run . next --state ../../state/plan.json
go -C tools/orchestratorctl run . status          # ...plus every step and its status
```

It runs every morning from `.github/workflows/orchestrator.yml` and sends the
answer to Telegram.

---

## Why this is a program and not a checklist

A checklist assumes step *N+1* becomes available when step *N* is ticked. This
plan does not work that way, and the clearest case is Reddit: the account has to
**age for fourteen days** before r/WebGames will accept a post, and no amount of
finishing the previous step makes that happen sooner.

So every step carries a `Blocker` that answers *why not yet, and when*:

```
blocked   Week 2   Post to r/WebGames
                   — the account is 3 days old; r/WebGames wants 14. Unblocks in 11 days.
```

That single line is the whole reason this exists. It is the difference between a
plan you re-read and a plan that tells you when to act.

---

## What it will not do

**One next action.** Not a list. A plan that offers five choices gets none of
them done, and the ordering already encodes which one matters.

**The earliest broken funnel stage, and only that one.** Improving stage four
while stage two is broken is work that cannot show up in the numbers — it is the
most common way a small product burns a month.

**It changes nothing.** Stage 1 (`observe`) on the [agentkit ladder](portable-agents.md).
The only file it writes is `state/orchestrator-last.json`, so that it does not
send the same message every morning.

---

## Three failure modes it is built around

### An unknown is not a zero

Every observation that could not run sets its field **negative**, never zero.
"I could not reach itch.io" and "you have published nothing" are the same number
in a naive implementation and they are opposite instructions to a human.

The first version of this tool got that wrong: offline, it reported
`XX distribution 0/6 games` — turning *I could not look* into *you have shipped
nothing*. `TestUnreachableItchIsBlindnessNotFailure` exists because of it.

When a stage cannot be seen, the diagnosis **stops there** and says so. A funnel
that skips the stage it cannot see will confidently blame the next one.

### A declaration that reality contradicts is a conflict

Some steps cannot be verified by anything (did you play it on your phone?), so a
human declares them in `state/plan.json`. Where a check *does* exist and
disagrees, that is reported loudly:

```
CHECK   Publish Sudoku on itch
        you marked this done, but 0/6 games live
```

Six separate bugs in this repository were something reporting success for work
that did not happen. A plan tracker is an easy seventh.

### Silence must be distinguishable from death

The digest arrives **once a day, every day** — and immediately whenever it
changes.

It used to be "on change, or every seven days", and seven was the alerter's
number, borrowed by mistake. The Reliability agent should stay quiet when
nothing is wrong: it interrupts a person, and an alert that fires on a calm day
gets muted.

This is not an alerter. It is a **morning briefing**, and a briefing that
arrives only when something changed is one nobody can rely on — its absence is
unreadable. On a day with nothing to report it says so in a line, and that line
is the evidence the fleet is alive.

The heartbeat is measured in **calendar days, not elapsed hours**. GitHub's
scheduler drifts by tens of minutes, so a 24-hour rule silently skips a morning
whenever yesterday's run was late and today's was early — and a skipped morning
is exactly what a dead agent looks like.

### 09:00 in Israel, all year

GitHub's scheduler only speaks UTC, and Israel is UTC+3 in summer and UTC+2 in
winter — so a single slot drifts to 08:00 every October, and nobody notices
until the briefing starts arriving before the coffee.

Two slots are scheduled, `06:00` and `07:00` UTC, and the job decides which one
is really morning:

| | 06:00 UTC | 07:00 UTC |
|---|---|---|
| summer (IDT) | **09:00 — sends** | 10:00 — already sent today |
| winter (IST) | 08:00 — too early | **09:00 — sends** |

The gate is **"not before 09:00 local"**, not "exactly 09:00". GitHub's
scheduler is routinely tens of minutes late, and an exact-hour test would drop
the briefing on precisely the mornings the runner was busy. Sending once per day
is enforced in the tool, so the redundant slot is a no-op rather than a
duplicate — late is recoverable, missing is not.

A **manual** run never checks the clock. If someone asks for the briefing at
midnight, they want the briefing.

---

## What it can and cannot see

| Fact | How |
|---|---|
| Games published on itch | reads the profile page and counts distinct project links |
| Sessions, replays, revenue | our own event stream, last 7 days, `env=lab` excluded |
| What the fleet is waiting on | `gh pr/issue list --label agent` |
| Reddit account age | **declared** — see below |
| itch views and plays | **declared** — see below |

### Reddit

Reddit answers **403** to unauthenticated JSON from anything that is not a
browser, so the live check fails from CI every time — by design, not by
accident. If that were allowed to block the schedule, `reddit-post` would stay
blocked forever, and *a scheduler that can never advance is worse than no
scheduler*.

So `reddit_created_on` is a date typed by a human, and the API is attempted only
as a bonus.

### itch

itch publishes no API, and our itch builds are served by **itch's** servers, not
ours — so a play on itch is invisible to us. Views and plays are copied off the
dashboard by hand into `state/plan.json`, and treated as **unknown** once they
are more than ten days old rather than reasoned from stale.

This is a real hole: two of the six funnel stages depend on numbers nobody is
obliged to type in. The fix is a beacon in the itch build, which would make
off-platform plays first-party data — and it is the same argument as the SDK
conversation at Rise: **off-platform distribution needs its own measurement or
you are flying blind.**

---

## The funnel

```
distribution → reach → click → play → return → revenue
```

| Stage | Healthy | What a failure there actually means |
|---|---|---|
| distribution | ≥1 game live | there is nowhere to find this |
| reach | ≥50 views/week | the pages exist and nobody is finding them |
| click | ≥25% view→play | the cover image is the pitch, and it is not working |
| play | ≥35% replay | the first thirty seconds, not content volume |
| return | ≥15% returning | they play once — the daily is the only designed reason to come back |
| revenue | > $0 | an ad stack question, not a product one |

### Too early is not broken

The first reading after publishing was **one view across six games**, hours old,
and the funnel called reach broken. True, and useless: nothing had had time to
happen.

A stage is now reported as `too early` when the number *was* read but the pages
have not existed long enough for it to mean anything — seven days, for reach.
Three states, not two:

| | meaning |
|---|---|
| `?` | the signal could not be read at all |
| `···` | read, and nothing has had the chance to happen yet |
| `XX` | read, enough time has passed, and it is bad |

And the diagnosis **stops** at a too-early stage rather than reading past it.
Every later stage depends on this one having had a chance, so judging them now
would be measuring the clock in four more places.

Not knowing when the pages went live is never an excuse: with no publication
date, the numbers are judged normally.

`play` and `return` come from [audience.md](audience.md), which was written
before there was any data to be disappointed by. That is the only reason they
are worth anything.

### The `return` stage, and how it stopped being blind

It was blind. `adlab.js` kept session identity in `sessionStorage` only, so a
visitor on Tuesday and the same visitor on Thursday were two strangers, and the
15% threshold the whole week-six decision rests on could not be evaluated at
all.

The fix is not a persistent identity. It is a **boolean and a bucket**.

The browser stores one date locally — `YYYY-MM-DD`, first-party, **never
transmitted** — and sends only which bucket the gap falls into: `new`, `d0`,
`d1_7`, `d8_30`, `d30_plus`. Every returning browser sends the identical string,
so the transmitted value cannot single anyone out or be joined to anything.

**Compute on the client, transmit the answer rather than the data.** It is the
same principle as on-device attribution, and it is the difference between
answering the question and building a profile to answer it with. There is a
browser assertion that the payload contains no date, because a privacy property
nobody tests is a privacy intention.

Only `d0` and `d1_7` count toward the seven-day rate. A visitor last seen two
months ago is a returning *human*, not a returning-*this-week* one, and counting
them would drift the number upward forever as the site aged.

**A session whose browser blocked storage is in neither the numerator nor the
denominator.** The field is omitted rather than sent as `false`, because
counting those as "did not return" would understate the rate by exactly the
number of privacy-conscious players — who are not a random sample.

---

## Checking the fleet

Whether an agent *could* run is a much less interesting question than whether it
*did*. This report answered the first one for a while, and on the day it was
finally asked the second, the answer was:

```
HEALTH  7 of 10 agents are not running
           Orchestrator  ok            ran 0h ago
        XX Yield         failing       last run of agent-proposals.yml: failure
           Integrity     ok            ran 10h ago
        XX FinOps        not scheduled built, tested, and nothing runs it
        XX Demand        not scheduled built, tested, and nothing runs it
           Reliability   ok            ran 1h ago
        XX Product       not scheduled built, tested, and nothing runs it
        XX Feedback      failing       last run of agent-proposals.yml: failure
        XX Horizon       never run     horizon.yml has no runs at all
        XX Supply chain  not scheduled built, tested, and nothing runs it
```

Four agents were **built, tested, documented and run by nothing**. Two more had
never executed because `agent-proposals.yml` did not parse. None of it failed
loudly, and the report above it said the fleet was fine.

Five states, and the distinctions matter:

| state | meaning |
|---|---|
| `ok` | ran within its cadence |
| `not scheduled` | built and tested, and no workflow runs it |
| `never run` | a workflow exists and has never executed |
| `failing` | the last run did not succeed |
| `overdue` | last success older than **twice** its cadence |
| `unknown` | GitHub could not be reached — never reported as healthy |

Overdue is deliberately lenient. A check that reports at the first missed beat
gets muted, and a muted health check is worse than none.

**An agent that cannot report its own liveness is one you find out about on the
day you needed it.** That is why health is printed above the funnel: a diagnosis
drawn from an agent that has not run is worse than no diagnosis.

## Scheduling the fleet

Most of the fleet needs traffic, and there is none yet. But not all of it:

```
run   Reliability   alarms and spend, twice a day
run   Product       game and feature proposals, benchmarked against what is out there
run   Feedback      itch and Reddit comments the data agrees with
run   Supply chain  verification
wait  Yield         needs traffic — which arm of an experiment is winning
wait  Integrity     needs traffic — which impressions should not have been billed
wait  FinOps        needs traffic — what each publisher and buyer costs us
wait  Demand        needs traffic — which buyers are worth the call
```

Leaving the outward-looking agents idle because the *data-driven* ones are idle
is a scheduling mistake, not a data one. "The fleet is quiet" is only reassuring
once you know which half of it is quiet on purpose.

---

## `state/plan.json`

Deliberately tiny. Every field here is a fact nothing can check, and an
unverifiable fact is a place for the plan and reality to drift apart.

```json
{
  "started_on":       "2026-08-29",
  "itch_user":        "",
  "reddit_user":      "",
  "reddit_created_on": "",
  "done":             {},
  "itch_views_7d":    -1,
  "itch_plays_7d":    -1,
  "itch_stats_as_of": ""
}
```

`-1` means *unknown*. `0` means *checked, and genuinely zero*. They are never
the same thing.

To mark a step done that nothing can verify, add its id:
`"done": { "phone-test": true }`.
