---
name: new-game
description: Add a new game to the Publisher, or change an existing one, so it meets the Game Standard. Use whenever adding, modifying or reviewing a game in apps/publisher.
---

# Adding or changing a game

Read `docs/game-standard.md` first. This skill is the procedure; that document
is the reasoning. Do not skip the test at the end — it is the whole point.

## Before writing anything

Answer in one line each:

1. **What is the one-sentence rule?** If it needs two sentences, the game is
   too complex for this site.
2. **What is the input?** swipe / tap / arrows — it must work with both touch
   and keyboard.
3. **Is it beatable?** If there is an AI, can a human win? If not, name the
   level honestly (see the Perfect rule).
4. **What is the board's aspect ratio?** It determines the `--chrome` budget.

## Build order

1. **Logic module** — `apps/publisher/static/<game>.js`, an IIFE that returns
   early if its root element is absent. No imports from `services/`. Use
   `window.GameRoom` helpers (`beep`, `tune`, `confetti`, `say`, `getVar`) —
   do not reimplement sound, confetti or colour lookups.
2. **Page** — `apps/publisher/<game>/index.html`. Copy the head block from
   `2048/index.html` and change canonical, title, description and OG tags.
3. **Styles** — append to `static/styles.css`. The board **must** be sized
   `min(<vw>, <max>px, calc(100dvh - var(--chrome)))`. Never width alone.
4. **Analytics** — emit exactly `game_start`, `game_end`, `game_replay`.
5. **Ad slot** — one `#ad-slot` below the board, labelled. The game must play
   with the ad server down.

## Wire it in — the list people forget

- `nav` in `tools/test/games.json`, then the nav block on **every** page
- Home page card in `index.html`: art, name, description, meta
- `sitemap.xml`
- `ASSETS` in `tools/build/hash-assets.py`
- `games` contract in `tools/test/games.json` — `requires`, `play`, `expect`.
  `expect` must prove the game *responded to input*, not merely rendered.

## Verify — always, no exceptions

```
make test-site
```

96 assertions across a laptop and an iPhone viewport: pages serve, nav is
complete and unclipped, required elements exist, the board renders, real key
and click input changes it, the board is above the fold, and no page logs an
error.

Then **look at it**. Capture a 390px-wide screenshot and review it by eye. The
test proves the game works; only your eyes prove it looks good.

If the nav now overflows because this is the fifth game, switch to short mobile
labels — do not shrink the font again.

## When changing an existing game

Same rule: `make test-site` before and after. If a `--chrome` value changes,
re-measure rather than guessing, and say so in the commit message.
