#!/usr/bin/env python3
"""Render an itch.io cover image for each game, at exactly 630x500.

    python3 tools/build/itch-cover/render.py

WHY THIS EXISTS

We had none. Six ZIPs were ready to upload and neither dist/itch/LISTINGS.md nor
docs/GO-LIVE.md mentioned a cover image -- so every listing would have shipped
with itch's default placeholder.

That is not a cosmetic gap. On an itch browse page the cover is very nearly the
whole pitch: a grid of thumbnails is the only thing between a player and the
game, and `productctl benchmark` scores the view->play step as its own funnel
stage for exactly this reason. Uploading six games with no covers would have
broken the funnel one stage before anyone could play anything.

630x500 is itch's recommended cover ratio (315:250). Anything else gets cropped
by itch, usually through the title.

Rendered in the browser we already drive for tests, reusing the site's own
icons and palette, so a cover cannot drift from the game it advertises.
"""
import pathlib, sys

sys.path.insert(0, "tools/test")
sys.path.insert(0, "tools/build")
from cdp import Browser                                        # noqa: E402
from icons import ICONS                                        # noqa: E402

HERE = pathlib.Path(__file__).parent
OUT = pathlib.Path("dist/itch/covers")

# One hook per game. Short, concrete, and about the FEELING rather than the
# rules -- a browse page gives you about a second, and "Nine boxes. Three in a
# row." tells someone what they already know.
GAMES = [
    ("sudoku",       "sudoku",       "Sudoku",        "A fresh puzzle every day",   "#F2622E"),
    ("2048",         "2048",         "2048",          "Slide, merge, chase 2048",   "#FFB627"),
    ("snake",        "snake",        "Snake",         "One apple at a time",        "#2BC4AE"),
    ("memory",       "memory",       "Memory",        "Find every pair",            "#7C5CFF"),
    ("tic-tac-toe",  "xo",           "Tic-Tac-Toe",   "Beat an AI that never slips", "#E8434F"),
    ("connect-four", "connect-four", "Connect Four",  "Four in a row, on gravity",  "#4A9BE8"),
]

TEMPLATE = (HERE / "template.html").read_text()


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    b = Browser(630, 500)
    failures = []
    try:
        for slug, icon_key, title, hook, accent in GAMES:
            icon = ICONS.get(icon_key)
            if not icon:
                failures.append(f"{slug}: no icon named {icon_key!r} in tools/build/icons.py")
                continue
            html = (TEMPLATE
                    .replace("__ICON__", icon)
                    .replace("__TITLE__", title)
                    .replace("__HOOK__", hook)
                    .replace("__ACCENT__", accent))
            tmp = OUT / f".{slug}.html"
            tmp.write_text(html)
            b.goto("file://" + str(tmp.resolve()))
            # A screenshot taken mid-layout is a subtly wrong image nobody
            # notices until it is public.
            b.eval("document.fonts ? document.fonts.ready.then(function(){return 1}) : 1")
            path = OUT / f"{slug}.png"
            b.shot(str(path))
            tmp.unlink()

            size = path.stat().st_size
            print(f"  {path}  ({size/1024:.0f} KB)")
            # A blank render is a valid PNG. Only its size gives it away, and
            # this whole file exists because a missing image shipped once.
            if size < 8_000:
                failures.append(f"{slug}: {size} bytes — did it render?")
    finally:
        b.close()

    if failures:
        print("\nFAILED", file=sys.stderr)
        for f in failures:
            print("  " + f, file=sys.stderr)
        return 1
    print(f"\n{len(GAMES)} covers in {OUT}/ — upload one per game as the cover image.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
