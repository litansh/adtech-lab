# Go live

Everything else is finished. This is the entire remaining list.

**About 90 minutes in one sitting.** Three steps in order — itch, Reddit,
affiliate. The order matters more than the timing: each one makes the next
stronger.

Keep two things open beside your browser:

- **`dist/itch/LISTINGS.md`** — all the itch text, one block per game
- **`docs/distribution-playbook.md`** — the Reddit post

> ~~Confirm the two AWS emails~~ ✅ **done** — cost and fuse alerts now reach
> `<alert-email>`.

---

# Step 1 — itch.io · 45 minutes

## 1.1 Create the account · 2 min

Go to <https://itch.io/register>. Any username. Confirm the email it sends.

**If you sign up with GitHub or Twitter instead, you must still verify your
email by hand** at <https://itch.io/user/settings>. OAuth hands itch your
address without itch ever having confirmed it, so the register-and-confirm flow
never runs — and nothing tells you until the first upload fails with:

> There was a problem completing your upload. Please verify your email address
> before uploading a file

**Ignore the "become a seller" prompt.** <https://itch.io/user/settings/seller>
exists to take money: payouts, VAT, tax forms. Our games are free and priced
**No payments**, and every pound this project earns is earned on xoxoxo.live,
never on itch. Handing over tax and payment details for a free listing is
answering a question nobody asked.

## 1.2 Create the first game · 7 min

Go to <https://itch.io/game/new>.

Open **`dist/itch/LISTINGS.md`** and use **section 1 — Sudoku**. Fill the form
top to bottom:

| Form field | What to do |
|---|---|
| **Title** | Paste the Title line from LISTINGS section 1 |
| **Project URL** | Type `sudoku` |
| **Short description** | Paste the short description block |
| **Classification** | Select **Game** |
| **Kind of project** | Select **HTML** |
| **Release status** | Select **Released** |
| **Pricing** | Select **No payments** |
| **Cover image** | upload `dist/itch/covers/<game>.png` — 630×500, already rendered |
| **Uploads → Upload files** | Choose `dist/itch/sudoku.zip` |
| ↳ after it uploads | **Tick "This file will be played in the browser"** |
| **Embed options → Viewport** | `400` wide × `700` tall |
| ↳ | **Tick "Mobile friendly"** |
| ↳ | **Tick "Automatically start on page load"** |
| **Description** (the big editor) | Paste the whole Description block |
| **Genre** | Select **Puzzle** |
| **Tags** | Paste the tag list from LISTINGS |
| **Links → More information** | `https://xoxoxo.live` |
| **Visibility** | Select **Public** |

Click **Save & view page**.

> **Then check the Visibility field actually says Public.**
>
> itch defaults a new project to **Draft** and puts that field at the very
> bottom of a long form, so it is the easiest one to miss. A draft plays
> perfectly for *you*, because you are logged in as its owner — and 404s for
> everyone else, and never appears on your profile.
>
> **Playing it yourself is not evidence that it is published.** The cheap test
> is to open the page in a private window, or just ask me to check it.

## 1.3 Play it on your phone · 1 min

Open the page you just published on your phone and play a few squares.

You only need to do this **once**. An itch embed is a different environment from
our site, and thirty seconds tells you whether the whole batch will work. If
Sudoku plays, the other five will.

## 1.4 Repeat for the other five · 7 min each

Same form, same settings. Only these change, and every one of them is in the
matching LISTINGS.md section:

**Title · Project URL · Short description · Description · Tags · Cover**

| # | LISTINGS section | Project URL | ZIP | Cover | Genre |
|---|---|---|---|---|---|
| 2 | 2048 | `2048` | `dist/itch/2048.zip` | `covers/2048.png` | Puzzle |
| 3 | Snake | `snake` | `dist/itch/snake.zip` | `covers/snake.png` | **Action** |
| 4 | Memory | `memory` | `dist/itch/memory.zip` | `covers/memory.png` | Puzzle |
| 5 | Tic-Tac-Toe | `tic-tac-toe` | `dist/itch/tic-tac-toe.zip` | `covers/tic-tac-toe.png` | Puzzle |
| 6 | Connect Four | `connect-four` | `dist/itch/connect-four.zip` | `covers/connect-four.png` | Puzzle |

All covers live in `dist/itch/covers/` and are named after the ZIP. The cover is
the easiest field to skip, because it is the only one that is not text to paste
— and it is the one the browse page actually shows.

Everything else — HTML, Released, No payments, 400×700, mobile friendly,
auto-start, the xoxoxo.live link, Public — is identical every time.

---

# Step 2 — Reddit · 15 minutes

## 2.1 Read the rules · 3 min

Open <https://www.reddit.com/r/WebGames/> and read the sidebar rules.

They change, they are enforced, and a ban costs far more than the post gains. If
the rules say your account is too new, **stop here and come back in a week** —
spend that week commenting on other people's posts.

## 2.2 Post · 5 min

Open **`docs/distribution-playbook.md`**, section **"Step 3 — r/WebGames"**.

- Paste the **Title** as the post title
- Paste the **Body** as the post body
- The link inside the body is **`https://xoxoxo.live/daily`** — leave it as the
  daily, not the homepage. The daily shows the thing worth talking about within
  one screen; the homepage shows a list and asks the visitor to make a choice
  they have no reason to make yet.

## 2.3 Stay in the thread · 7 min, and again later

Reply to **every** comment, including the critical ones, especially the critical
ones. This is the difference between a post that does well and one that
vanishes.

Check back a few times over the next day.

---

# Step 3 — Affiliate · 30 minutes

## 3.1 Read the guide · 10 min

Open **`docs/affiliate-guide.md`**. It explains how the money actually reaches
your bank account, which is the part nobody writes down.

## 3.2 Pick one · 5 min

**Do not start with Amazon Associates.** It requires three qualifying sales
within 180 days or the account closes permanently and cannot be reinstated.
With a site this new that is a near-certain closure.

The guide lists networks with no volume requirement that accept small sites.
Pick one.

## 3.3 Apply · 15 min

Apply with the site as it now stands. Doing this **after** steps 1 and 2 is
deliberate: an application that points at a live site with six itch listings and
a Reddit thread is materially stronger than one pointing at nothing.

---

# What runs by itself afterwards

Nothing else needs you.

- Merging any PR deploys the site and the ad server
- The nightly job runs the cold-path agents and commits a report
- The Reliability agent checks spend and alarms twice a day and messages
  Telegram **only when something is wrong**
- When an agent finds something worth changing, it opens a pull request and
  sends you a message with a button straight to it

# When to look at the numbers

**Not for six weeks.** These thresholds are in `docs/audience.md` and were
written before either of us had a stake in the answer, which is the only reason
they are worth anything:

| Metric | Keep going if |
|---|---|
| returning sessions (7-day) | above 15% |
| `game_replay` rate | above 35% |
| sessions per week | trending up at all |

If after six weeks returning sessions are under 10% and weekly sessions are
flat, the honest conclusion is that this audience does not exist for this
product. The right response then is to scale back to affiliate-only — not to
post more.

# One thing to watch

The agents will be quiet at first, and that is correct: they have nothing to
report. But **silence is also what a broken agent produces.** If the site has
had traffic for a week and Telegram is still silent, that is worth me checking
rather than you trusting.

# The files you upload

All six rebuilt and verified today — unpacked, served, loaded in a browser, and
checked for a rendering board, no console errors, no ad tag, no analytics, and a
working link back to xoxoxo.live.

```
dist/itch/sudoku.zip          22 KB
dist/itch/2048.zip            23 KB
dist/itch/snake.zip           24 KB
dist/itch/memory.zip          22 KB
dist/itch/tic-tac-toe.zip     19 KB
dist/itch/connect-four.zip    19 KB
```

There is deliberately **no daily.zip**. The daily's whole mechanic is that
everyone plays the same puzzle and shares a comparable result, which needs one
canonical place. A copy on itch would generate its own puzzles from its own
clock and quietly fragment the thing that makes it work.
