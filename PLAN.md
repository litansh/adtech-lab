# The plan

**Start here.** One page, in order, over about six weeks. Everything else is
detail you only open when a step says to.

Each step says what to do, roughly how long, and which file has the text to
paste. Nothing here needs code.

---

## Today · about 60 minutes

### ☐ 1. Create a Reddit account — 2 minutes, do this first

<https://www.reddit.com/register>

You will not post for two weeks. Reddit treats brand-new accounts as spam, and
the gaming subreddits enforce it. **The account needs to age**, so create it now
and let it sit while you do everything else.

If you already have one you use normally, skip this — an aged account is worth
more than a clean one.

### ☐ 2. Create an itch.io account — 2 minutes

<https://itch.io/register>

If you sign up with GitHub instead, **verify your email by hand** at
<https://itch.io/user/settings> — otherwise the first upload fails, and the
error does not arrive until then.

**Skip anything about becoming a seller.** That is for charging money; our games
are free.

### ☐ 3. Publish Sudoku on itch — 10 minutes

**→ Open `docs/GO-LIVE.md`, step 1.2.** It lists every form field and what goes
in it.

The text to paste is in **`dist/itch/LISTINGS.md`, section 1**.
The file to upload is **`dist/itch/sudoku.zip`**.
The cover image is **`dist/itch/covers/sudoku.png`**.

The cover is not decoration. On an itch browse page it is very nearly the whole
pitch — a grid of thumbnails is all that stands between a player and the game —
so a listing left with itch's default placeholder is a game nobody clicks.

> **Check Visibility says Public before moving on.** itch defaults new projects
> to Draft, at the bottom of a long form. A draft plays fine for you and 404s
> for everyone else — playing it yourself is not evidence it is published.

### ☐ 4. Play it on your phone — 1 minute

Open the itch page you just made, on your phone, and play a few squares.

Do this once. An itch embed is a different environment from our site, and if
Sudoku works there, the rest will.

### ☐ 5. Publish the other five — 35 minutes

**→ `docs/GO-LIVE.md`, step 1.4** has the table: which LISTINGS section, which
URL, which ZIP, which genre. Only five fields change per game; everything else
is identical.

Order: 2048, Snake, Memory, Tic-Tac-Toe, Connect Four.

**If you run out of time or patience, stop after 2048.** Sudoku and 2048 carry
the largest search demand on itch, and the rest can wait a week.

---

## This week · 5 minutes total

### ☐ 6. Spend ten minutes on Reddit, twice — not posting

Comment on other people's posts in r/WebGames, r/playmygame, or anywhere you
actually find interesting. Two or three real comments.

This is not a growth trick. An account with no history posting its own link is
what the rules are written to stop, and this is the cheapest way not to be that.

### ☐ 7. Look at the itch numbers once — 2 minutes

itch.io shows views and plays per project. You are looking for one thing: **did
anyone play?**

Do not act on the answer yet. One week of an unadvertised itch page is not a
verdict on anything.

---

## Week 2 · about 30 minutes

### ☐ 8. Post to Reddit — 15 minutes

**→ `docs/distribution-playbook.md`, "Step 3 — r/WebGames".** The title and body
are drafted — **but rewrite them in your own words before posting.**

The top comment on a game post in that subreddit the week you read this was
*"Everything about this is gen AI. Even the text is also written by AI."* The
draft was written by Claude, which is the problem. The playbook's "make it
yours" section says exactly what to change.

**Read the sidebar rules first.** They change and they are enforced.

The link in the post is `https://xoxoxo.live/daily` — the daily, not the
homepage.

### ☐ 9. Reply to every comment — 15 minutes, and again the next day

Including the critical ones. Especially those. This is the difference between a
post that does well and one that vanishes.

---

## Week 3 · about 30 minutes

### ☐ 10. Apply to an affiliate programme

**→ `docs/affiliate-guide.md`.** It explains how the money reaches your bank
account, which is the part nobody writes down.

**Not Amazon Associates.** Three qualifying sales in 180 days or the account
closes permanently and cannot be reinstated.

Doing this now rather than on day one is deliberate: an application pointing at
six live itch listings and a Reddit thread is a far stronger one than an
application pointing at nothing.

---

## Week 6 · the decision · 20 minutes

### ☐ 11. Read the numbers properly, once

The thresholds are in **`docs/audience.md`** and were written before either of
us had a stake in the answer. That is the only reason they are worth anything:

| Metric | Keep going if |
|---|---|
| returning sessions (7-day) | above **15%** |
| `game_replay` rate | above **35%** |
| sessions per week | trending up at all |

**If returning sessions are under 10% and weekly sessions are flat**, the honest
conclusion is that this audience does not exist for this product. The right
response is to scale back to affiliate-only — **not to post more**.

That is a real possible outcome and it is written down in advance so it can be
acted on rather than argued with.

---

---

## What happens without you

- Merging any pull request deploys the site and the ad server
- The nightly job runs the cold-path agents and commits a report
- The Reliability agent checks spend and alarms twice a day and messages
  Telegram **only when something is wrong**
- When an agent finds something worth changing it opens a pull request and
  messages you with a button to it

**The agents will be silent at first, and that is correct** — they have nothing
to report until there is traffic. But silence is also what a broken agent
produces, so if the site has had visitors for a week and Telegram is still
quiet, say so and I will check.

---

## Which file, for what

| You need | Open |
|---|---|
| Every itch form field, spelled out | `docs/GO-LIVE.md` |
| itch titles, descriptions, tags | `dist/itch/LISTINGS.md` |
| The Reddit and Hacker News posts | `docs/distribution-playbook.md` |
| Choosing an affiliate, and how you get paid | `docs/affiliate-guide.md` |
| Why the daily matters more than more games | `docs/audience.md` |
| What the whole system is | `README.md` |
| Everything learned, with evidence | `docs/insights.md` |
