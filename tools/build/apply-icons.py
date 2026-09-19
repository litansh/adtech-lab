#!/usr/bin/env python3
"""Inject the shared icons into the nav and the home cards.

They live in three places -- nav, home cards, OG template -- and were three
independent copies before. This makes tools/build/icons.py the single source
and regenerates the other two, so a redesign is one edit rather than three.
"""
import pathlib, re, sys
sys.path.insert(0, "tools/build")
from icons import ICONS, NAV                                   # noqa: E402

SRC = pathlib.Path("apps/publisher")

CARD_TEXT = {
    "xo": ("Tic-Tac-Toe", "Three in a row. Play a friend, or a computer that never loses.",
           "2 players &middot; vs computer"),
    "connect-four": ("Connect Four", "Drop discs, connect four. Harder than it looks on Hard.",
                     "2 players &middot; vs computer"),
    "snake": ("Snake", "Eat, grow, don't bite yourself. Levels make each apple worth more.",
              "solo &middot; levels"),
    "2048": ("2048", "Join matching tiles until you reach 2048. One more go, every time.",
             "solo &middot; undo"),
    "sudoku": ("Sudoku", "One to nine, once each. Always solvable by logic, never by guessing.",
               "solo &middot; 3 levels"),
    "memory": ("Memory", "Find every pair in as few moves as you can.", "solo &middot; 3 sizes"),
}


def nav_html(active):
    out = ['<nav class="tabs" aria-label="Games">']
    for href, label, short, icon in NAV:
        cur = ' aria-current="page"' if href == active else ""
        out.append(f'    <a class="tab" href="{href}"{cur}>')
        out.append(f'      <span class="ico" aria-hidden="true">{ICONS[icon]}</span>')
        out.append(f'      <span class="lbl">{label}</span>'
                   f'<span class="lbl-short">{short}</span>')
        out.append("    </a>")
    out.append("  </nav>")
    return "\n".join(out)


def main():
    pages = {"index.html": None, "privacy/index.html": None}
    for href, _, _, icon in NAV:
        pages[href.lstrip("/") + "/index.html"] = href

    pat = re.compile(r'<nav class="tabs".*?</nav>', re.S)
    for path, active in pages.items():
        f = SRC / path
        if not f.exists():
            print(f"  skip {path} (missing)")
            continue
        s = f.read_text()
        s2, n = pat.subn(nav_html(active), s, count=1)
        if n != 1:
            print(f"  WARNING: no nav found in {path}", file=sys.stderr)
            return 1
        f.write_text(s2)
        print(f"  nav {path}")

    # Home cards: replace only the art, leaving the copy alone.
    h = SRC / "index.html"
    s = h.read_text()
    for icon, (name, _, _) in CARD_TEXT.items():
        href = "/" + ("xo" if icon == "xo" else icon)
        card = re.compile(
            r'(<a class="game-card" href="' + re.escape(href) + r'">\s*'
            r'<span class="gc-art" aria-hidden="true">).*?(</span>)', re.S)
        s2, n = card.subn(lambda m: m.group(1) + "\n        " + ICONS[icon] + "\n      " + m.group(2), s)
        if n == 1:
            s = s2
            print(f"  card {href}")
        else:
            print(f"  WARNING: no card for {href}", file=sys.stderr)
    h.write_text(s)
    return 0


if __name__ == "__main__":
    sys.exit(main())
