#!/usr/bin/env python3
"""
Build standalone, self-contained ZIPs of each game for itch.io.

itch.io serves an uploaded ZIP from a subdirectory, so absolute paths like
/static/styles.css break. Everything here is rewritten to relative paths and
flattened next to index.html.

Two deliberate differences from the live site:

  - No ad tag. itch.io hosts the files, our ad server is not in the path, and
    shipping a tag that always fails would be noise. The point of itch is
    discovery, not monetisation.
  - No analytics. Same reason -- events from a host we do not control would mix
    a third party's traffic into our own numbers.

Instead each build carries a link back to xoxoxo.live, which is the whole
purpose: itch.io is a discovery channel that sends players to the site where
the ad server does run.
"""
import pathlib, re, shutil, zipfile

SRC = pathlib.Path("apps/publisher")
OUT = pathlib.Path("dist/itch")
SITE = "https://xoxoxo.live"

GAMES = {
    "tic-tac-toe": ("xo", ["styles.css", "game-room.js"]),
    "connect-four": ("connect-four", ["styles.css", "game-room.js"]),
    "snake": ("snake", ["styles.css", "game-room.js", "snake.js"]),
    "2048": ("2048", ["styles.css", "game-room.js", "g2048.js"]),
    "sudoku": ("sudoku", ["styles.css", "game-room.js", "sudoku.js"]),
    "memory": ("memory", ["styles.css", "game-room.js", "memory.js"]),
}

# The daily puzzle is deliberately NOT packaged. Its whole mechanic is that
# everyone plays the SAME puzzle and shares a comparable result -- which needs
# one canonical place. A copy on itch.io would generate its own puzzles from
# its own clock and quietly fragment the thing that makes it work.

# The backlink is the only commercial mechanism in an itch build: no ad tag runs
# here, so the entire economic purpose of being on itch is sending a player to
# the site where one does.
#
# It used to read "More free games at xoxoxo.live", which is a description
# rather than a reason. The Horizon agent's first scan found "daily" to be the
# loudest pattern in the whole survey -- 1805 points across 26 posts, ahead of
# every individual game mechanic -- and the daily puzzle is the one thing on our
# site that does not exist inside this ZIP. So the backlink now names it.
#
# This is also the only funnel step we control from inside itch: itch owns
# discovery and the play session, we own what happens after it.
BACKLINK = (
    '<footer class="site-foot">\n'
    f'  <p>A new puzzle every day &mdash; play today\'s at '
    f'<a href="{SITE}/daily" target="_blank" rel="noopener">xoxoxo.live/daily</a></p>\n'
    '</footer>\n'
)


def build(name, folder, assets):
    d = OUT / name
    if d.exists():
        shutil.rmtree(d)
    d.mkdir(parents=True)

    for a in assets:
        shutil.copy2(SRC / "static" / a, d / a)

    html = (SRC / folder / "index.html").read_text()

    # absolute -> relative, and flattened
    html = re.sub(r'(href|src)="/static/([^"]+)"', r'\1="\2"', html)
    html = re.sub(r'(href|src)="/(favicon\.svg|apple-touch-icon\.png|site\.webmanifest)"',
                  rf'\1="{SITE}/\2"', html)

    # Any remaining site-internal link becomes an absolute link back to the
    # site. On itch.io a relative "/daily" 404s; pointing it home is both
    # correct and the entire point of being there -- itch is a discovery
    # channel that should send players to where the ad server runs.
    html = re.sub(r'href="/([a-z0-9-]*)"', rf'href="{SITE}/\1"', html)

    # strip what must not ship: ad tag, analytics, service worker, manifest
    html = re.sub(r'\s*<script src="adtag\.js"></script>', '', html)
    html = re.sub(r'\s*<script src="adlab\.js"></script>', '', html)
    html = re.sub(r'\s*<script src="/static/adtag\.js"></script>', '', html)
    html = re.sub(r'\s*<script src="/static/adlab\.js"></script>', '', html)
    html = re.sub(r'<script>\s*/\* Register the service worker.*?</script>', '', html, flags=re.S)
    html = re.sub(r'\s*<link rel="manifest"[^>]*>', '', html)
    html = re.sub(r'\s*<div class="ad-slot".*?</div>\s*<p class="ad-label">.*?</p>', '', html, flags=re.S)

    # navigation between games does not exist inside a single-game zip
    html = re.sub(r'<nav class="tabs".*?</nav>', '', html, flags=re.S)
    html = re.sub(r'<p class="brand">.*?</p>', f'<p class="brand"><a href="{SITE}" target="_blank" rel="noopener">The Game Room</a></p>', html, flags=re.S)
    html = re.sub(r'<footer class="site-foot">.*?</footer>', BACKLINK, html, flags=re.S)

    leftovers = re.findall(r'(?:href|src)="/[^"]*"', html)
    if leftovers:
        raise SystemExit(f"{name}: absolute paths would 404 on itch.io: {leftovers}")

    (d / "index.html").write_text(html)

    zpath = OUT / f"{name}.zip"
    with zipfile.ZipFile(zpath, "w", zipfile.ZIP_DEFLATED) as z:
        for f in sorted(d.rglob("*")):
            if f.is_file():
                z.write(f, f.relative_to(d))
    kb = zpath.stat().st_size / 1024
    print(f"  {zpath}  ({kb:.0f} KB)")


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    for name, (folder, assets) in GAMES.items():
        build(name, folder, assets)
    print(f"\nUpload each ZIP to itch.io and tick \"This file will be played in the browser\".")
