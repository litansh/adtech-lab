# Game Standard

Every game on the Publisher meets this. It is not style preference — each rule
below exists because breaking it cost us something measurable, and most of them
are now enforced by `make test-site` rather than by memory.

Run `make test-site` after **every** game change. It takes ~20 seconds.

---

## 1. The fold rule

**The playable area must be fully visible without scrolling, on a 390×664 phone
and a 1280×676 laptop.**

This is the rule we have broken the most. A casual player who lands on a game
and sees a header, a title and half a board does not scroll — they leave. When
we last measured, every one of the four games was **160–224px below the fold on
a phone**.

The cause was always the same mistake: sizing a board by width only.

```css
/* wrong -- 92vw on a 390px phone is a 359px square, and the page has only
   ~340px of vertical room left after the chrome */
width: min(92vw, 460px);

/* right -- bounded on both axes */
width: min(92vw, 460px, calc(100dvh - var(--chrome)));
```

`--chrome` is the measured height of everything stacked above and below the
board (topbar, controls, status, hint, ad slot). It is set per breakpoint in
`styles.css` and **must only be changed together with a measurement**.

Use `dvh`, never `vh`. On iOS Safari `vh` refers to the viewport *without* the
address bar, so a `vh`-sized board is cut off until the user scrolls — the exact
bug it was meant to prevent.

## 2. The menu rule

**All game names fit on one row without scrolling or clipping, at every
viewport.** A nav strip clipped mid-word reads as a broken site.

On phones the topbar drops the site name (the selected tab already names the
game and the logo links home) and the per-game tab icons (~30px each). That took
the topbar from **157px to ~50px** on a 577px screen.

`site_check.py` asserts `.tabs` does not overflow its container. **A fifth game
will fail this test** rather than silently shrink the strip — at that point
switch to short mobile labels, don't shrink the font again.

## 3. Playable in one screen, learnable in one sentence

- No instructions screen, no tutorial, no sign-up, no modal before play.
- The status line states what to do right now in plain words
  ("X's turn", "Join the tiles to reach 2048"), not the game's internal state.
- One line of hint text under the board is the entire manual.

## 4. Input: both, always

Every game accepts **keyboard and touch**. Touch is primary — most traffic is
mobile.

- Swipe games (`2048`, `Snake`) also bind arrow keys and WASD.
- Click games (`XO`, `Connect Four`) have hit targets ≥ 44px.
- `touch-action: none` on the board only, never on the page.
- Never rely on hover for information a player needs.

## 5. Honest difficulty labels

An unbeatable minimax opponent labelled "Hard" reads as a broken game — players
conclude it cheats and leave. XO's hardest level is called **Perfect** and says
so:

> Perfect plays flawlessly — a draw is the best result available. Can you get one?

If a level cannot be beaten, name it something that cannot be beaten.

## 6. Forgiveness

Casual players quit at unfair losses, not at hard ones.

- One level of **undo** where a misinput can end a run (2048 has it).
- A win condition does not have to end the game (2048 offers "keep going").
- Never lose a player's best score. Persist it in `localStorage` under a
  game-specific key (`g2048_best`, `snake_best`). This is a score, **not an
  identifier** — see `docs/privacy-baseline.md`.

## 7. Feel

- Every meaningful action gets an animation ≤ 200ms and a sound.
- The sound toggle is global, persisted, and respected on the first frame.
- Honour `prefers-reduced-motion: reduce` — drop animations, keep the game.
- Pause on `visibilitychange` when hidden. A timed game that keeps running in a
  background tab destroys the player's best score for no reason.

## 8. Shared plumbing — do not reinvent

`game-room.js` owns the cross-game furniture. A new game **uses** it:

| Helper | Purpose |
|---|---|
| `beep(name)` / `tune(name)` | sound, already gated on the toggle |
| `confetti()` | win celebration on the shared `#confetti` canvas |
| `say(text)` | announce to `#live` for screen readers |
| `getVar(name)` | read a CSS custom property, so JS never hardcodes a colour |

## 9. Analytics contract

Exactly three events, via `adlab.js`. No others without a written reason.

| Event | When |
|---|---|
| `game_start` | first input of a game, not on page load |
| `game_end` | a terminal state, with outcome and duration |
| `game_replay` | player chooses to play again — our clearest engagement signal |

`game_replay` is the metric that answers "does advertising hurt the product?".
Every monetisation change is judged against it in the same experiment.

## 10. The ad slot

- Exactly one slot: `#ad-slot`, below the board, labelled "Advertisement".
- Never above the board. Never between the board and its controls.
- The game must be fully playable with the ad server unreachable. `adtag.js`
  failing is not allowed to break `game-room.js` — they share only a
  `session_id` and the analytics transport, and **must not import each other**.
- Never add a slot merely because we can. See the Product Quality rule in
  `CLAUDE.md`.

## 11. Accessibility floor

- Status region is `role="status"` `aria-live="polite"`.
- Board is reachable and operable by keyboard alone.
- Colour is never the only signal — Connect Four discs differ in position and
  label, not just red/yellow.
- Visible focus ring on every control.

## 12. Ship checklist

A game is not done until all of these are true:

- [ ] Page at `/<game>/index.html` with canonical, OG and Twitter tags
- [ ] Added to the nav on **every** page, in `tools/test/games.json` `nav`
- [ ] Home page card: art, name, one-line description, meta
- [ ] `sitemap.xml`
- [ ] Asset listed in `tools/build/hash-assets.py` `ASSETS`
- [ ] Contract added to `tools/test/games.json` `games`
- [ ] `make test-site` — **all green**
- [ ] Screenshot reviewed at 390px width, by eye, not just by assertion

---

## Adding a game

Use the skill: `/new-game`. It walks this document in order and ends by running
the test.

The contract in `tools/test/games.json` is the part people forget. It is what
turns this document from advice into a test:

```json
{
  "path": "/2048/", "name": "2048", "board": ".tf-wrap",
  "requires": ["#g2048", "#tf-score", "#tf-best", "#tf-status", "#tf-undo"],
  "play": [{"key": "ArrowLeft", "vk": 37}, {"key": "ArrowUp", "vk": 38}],
  "expect": "document.querySelectorAll('#g2048 .tf-tile').length >= 2"
}
```

`expect` must assert the game **responded to input** — not merely that it
rendered. "It rendered" passes on a game whose event handlers never bound.
