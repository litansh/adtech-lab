# Distribution playbook

Copy-paste, in order. Everything here needs a human account, which is why it is
yours to do and not something the build can automate.

**Before anything else, read this once:** the fastest way to get nothing out of
this is to drop links in five places in one evening. Reddit in particular bans
accounts that only ever post their own work. Space these out, and comment on
other people's posts in between.

---

## Step 0 — the two-minute sanity check

Open these on your phone, on mobile data, not on wifi:

- <https://xoxoxo.live/snake>
- <https://xoxoxo.live/xo>

Then paste `https://xoxoxo.live` into a WhatsApp message to yourself. You should
see a preview card with the three game tiles. If you do not, stop and tell me —
everything below depends on that card rendering.

---

## Step 1 — friends and family first

**Why first:** thirty real people will tell you whether the games are actually
fun. Every step after this assumes they are. If nobody plays a second round,
more distribution just spreads a weak product further.

Send to a group chat:

> Made a little games site — six games plus a daily puzzle, all free, no
> sign-up, works on phones. Would love to know if any of them are actually fun
> or if they get boring after two minutes.
>
> https://xoxoxo.live

Then **watch what happens** in the numbers:

```sql
-- analytics/queries/05-session-and-product.sql
-- games_per_session and replay_rate are the answer to "is this fun?"
```

Under 2 games per session means they are not. Fix that before Step 3.

---

## Step 2 — itch.io

Free, permanent, and a genuine discovery channel. **Do this before Reddit** —
having an itch page makes the Reddit post look like a project rather than a
drive-by link.

Packages are already built:

```bash
make itch          # writes dist/itch/*.zip
```

1. Sign up: <https://itch.io/register>
2. New project: <https://itch.io/game/new>
3. For **each** of the three games, one project each:

| Field | Value |
|---|---|
| **Title** | `Snake` (then `Tic-Tac-Toe`, `Connect Four`) |
| **Project URL** | `snake-gameroom` |
| **Short description** | `Classic Snake. Arrows, WASD or swipe. Three speeds, no sign-up.` |
| **Classification** | Games |
| **Kind of project** | **HTML** |
| **Release status** | Released |
| **Pricing** | No payments |
| **Upload** | `dist/itch/snake.zip` — then tick **"This file will be played in the browser"** |
| **Viewport** | `640 × 900`, tick *Fullscreen button* and *Mobile friendly* |
| **Genre** | Puzzle (Snake/Connect Four), Strategy (Tic-Tac-Toe) |
| **Tags** | `browser`, `casual`, `html5`, `mobile-friendly`, `no-download`, `singleplayer`, `snake`, `retro` |
| **Visibility** | Public |

4. In the description field, paste:

> Classic Snake in your browser. Arrow keys, WASD, or swipe on a phone.
>
> Three speeds. Your best score is saved locally — no account, no sign-up, no
> downloads, no ads on this page.
>
> More games at https://xoxoxo.live

**Fill in every tag field.** itch's search and recommendations strongly favour
complete listings; sparse ones never surface.

---

## Step 3 — r/WebGames

<https://www.reddit.com/r/WebGames/>

r/WebGames is one of the few subreddits that welcomes this — it exists for
playable browser links. Two conditions:

- **Read the sidebar rules first.** They change, and they are enforced.
- Your account should not be brand new with zero history. If it is, spend a week
  commenting on other posts before you post your own.

### Before you paste any of this — make it yours

**The top comment on a game post in r/WebGames, one day before you read this,
was:**

> "Everything about this is gen AI. Even the text is also written by AI"

It had more upvotes than the post. And a second comment praising the same game
was **downvoted to −1** for describing a feature the game does not have — it
read as written by someone who had not played it.

That is the reflex your post will meet, and **the text below was written by
Claude**, which is exactly the problem. Once "this is AI" is the top comment,
the thread is about that instead of about the games, and no reply recovers it.

So use the structure and throw away the sentences. Specifically:

- **Say you built it, and why**, in the way you would tell a friend. "I wanted
  somewhere to play a quick game without an install or an account" is true and
  it is yours; the polished version below is neither.
- **Cut the tidy bullet list of "deliberate choices".** Keep at most one, in
  your own words. A list of three elegant design decisions is the single
  strongest tell.
- **Leave the typos in.** Genuinely. A post with a slightly awkward sentence
  reads as a person; a post with none reads as generated.

**Your defence is unusually strong, and it should stay in your pocket.** The
games are hand-written JavaScript, the icons are hand-authored SVG, the covers
are rendered from those same icons, and the daily is a seeded generator —
anyone can check that the same date produces the same puzzle for everyone. If
the accusation comes, one short factual reply settles it. Leading with it does
not: nobody believes a defence offered before the charge.

---

**Title** *(a starting point — rewrite it):*

> I built a browser games site with a daily Sudoku — same puzzle for everyone, no sign-up

**Body** *(the shape is right; the words should be yours):*

> I wanted somewhere to play a quick game without an install, an account, or a
> cookie banner, so I built one.
>
> Six games: Tic-Tac-Toe, Connect Four, Snake, 2048, Sudoku and Memory. The
> thing I actually care about feedback on is the **Daily Sudoku** — one puzzle a
> day, the same one for everyone, with a spoiler-free result you can share:
>
> ```
> Sudoku #241
> 🟩🟩🟨
> 🟩🟩🟩
> 🟩🟨🟩
> 4:12 · 3 mistakes
> ```
>
> The grid is the puzzle's own 3x3 boxes — green where you solved it cleanly,
> yellow where you slipped. It says how you did without giving anything away.
>
> A few deliberate choices, in case they are interesting:
> - Every Sudoku is generated with exactly **one** solution, so you never have
>   to guess. If you are stuck there is always something to deduce.
> - The hardest Tic-Tac-Toe is called **Perfect** rather than Hard, because it
>   is unbeatable minimax and a draw is the best result available. Calling that
>   "Hard" makes it read as a broken game.
> - Snake has in-run levels where an apple is worth 10 x your level, so one deep
>   run beats twenty shallow ones.
>
> Everything runs client-side, works offline once loaded, no accounts, no
> tracking.
>
> https://xoxoxo.live/daily
>
> Genuinely after feedback on whether the daily is worth coming back for, and
> whether the share format makes sense to anyone who has not played it.

**Then stay in the thread and reply to every comment.** That is the difference
between a post that does well and one that vanishes.

---

### Step 3b — the channel the data points at

**Sudoku is 83% of every view these listings have had** — 20 of 24, with four of
six games on zero. That is not noise five days in: Sudoku is the one with a
daily, and `daily` is the loudest pattern in the whole Horizon survey.

So the second post should go where people already want exactly this, rather than
to another general games audience:

- **r/sudoku** — a community that would genuinely want a free daily sudoku with
  a shareable result. Read its rules first; some puzzle subreddits ban all
  self-promotion, and a ban there costs more than the post gains.
- **Show HN** — the daily's generator is the interesting part to that audience:
  seeded from the date, verified to have exactly one solution, so every past
  puzzle is reproducible and the archive costs nothing to store. That is a
  technical story, which is the only kind that lands there.

**Both of these lead with the daily, not with "six games".** Six games is a list;
one daily puzzle that is the same for everyone is a thing to talk about — and it
is the thing the numbers say people are turning up for.

Space them out. Two posts on the same day to different communities reads as a
campaign; a week apart reads as a person.

# Step 4 — r/playmygame

<https://www.reddit.com/r/playmygame/>

Explicitly for creators sharing their own work. Check the sidebar — this sub
often requires a specific post format and sometimes reciprocal feedback (you
play and comment on someone else's game).

Same title and body as Step 3.

---

## Step 5 — Hacker News, Show HN

<https://news.ycombinator.com/submit>

Only worth doing once, so do it after the games have had feedback and you have
fixed anything obvious.

**Title** — the `Show HN:` prefix is required and the rest must be plain:

> Show HN: Browser games with a daily Sudoku that is the same for everyone

**First comment**, posted by you immediately after submitting:

> I built this because every casual game site I found wanted an account, an app
> install, or consent to eleven trackers before letting me play Snake.
>
> Six games plus a daily Sudoku, all client-side. A few implementation notes
> since this is HN:
>
> The Sudoku generator removes cells one at a time and keeps a removal only if
> the puzzle still solves uniquely — so no puzzle ever requires a guess. The
> solution counter stops at two, because you never need to know a puzzle has
> nine solutions, only that it has more than one.
>
> The daily is seeded with mulberry32 from the UTC date, so everyone gets the
> same puzzle without a server deciding anything. The shareable result is the
> puzzle's own 3x3 boxes, coloured by where you made mistakes — comparable
> without being a spoiler.
>
> Tic-Tac-Toe's hardest level is called Perfect rather than Hard, because
> unbeatable minimax labelled "Hard" reads as a broken game rather than a
> solved one.
>
> No cookies, no third-party requests, and the only stored state is your best
> scores in localStorage — a score, not an identifier.
>
> It is also a testbed for an ad server I am building, which is why it serves
> its own ads rather than a pasted tag. The games work identically with ads
> disabled, and I would rather hear that they are boring than that the tech is
> interesting.

**HN rules that matter:** no vote manipulation, no asking anyone to upvote, and
answer critical comments straight. HN rewards honesty about what something is.

---

## Step 6 — keep going, quietly

- **r/incremental_games**, **r/IndieGaming** — read rules, they are stricter
- Small Discord servers for browser/indie games
- Post again when a new game launches, not before

---

## What to watch afterwards

```bash
# did anyone actually play, and did they come back for a second game?
analytics/queries/05-session-and-product.sql
```

| Metric | What it tells you |
|---|---|
| `sessions` | Did distribution work at all |
| `games_per_session` | **Under 2 = the games are not fun enough.** Fix before posting more |
| `replay_rate` | The single best signal that a game works |
| `avg_session_seconds` | Under 60s means they bounced |

**Do not add more monetisation until `games_per_session` is above 3.** Adding ads
to a product nobody replays just makes a weak product worse, and that is the MFA
trap the Product Quality Rule exists to prevent.

---

## What changed, and why the copy is different now

**Updated 2026-08-29.** The earlier version of this playbook advertised three
games and led with "no sign-up". Both were true and neither was a reason to
visit — every browser game site claims no sign-up.

The copy now leads with the **Daily Sudoku**, because it is the only thing here
that answers *"why come back tomorrow?"*, and the shareable result is the only
mechanism on the site by which one player brings another. `docs/audience.md`
argues that without those two, every acquisition channel is a leaky bucket —
traffic arrives, plays once, and leaves.

The technical notes in the Hacker News comment are deliberate too. HN responds
to a specific, checkable claim far better than to a description, and "the
generator keeps a removal only if the puzzle still solves uniquely" is a
sentence someone can disagree with. "Fun and simple" is not.

**Post the daily URL, not the homepage.** `xoxoxo.live/daily` shows the thing
worth talking about within one screen; the homepage shows a list and asks the
visitor to choose, which is a decision they have no reason to make yet.
