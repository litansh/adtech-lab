#!/usr/bin/env python3
"""One source of truth for the game icons.

They appear in three places -- the nav on every page, the home page cards, and
the Open Graph preview image -- and were previously three separate copies of
flat geometric shapes. Three copies drift, and flat shapes read as 2016.

The house style here is "soft 3D": one saturated colour per game, a darker base
offset downward so every object sits on something, a light source consistently
top-left, and generous corner radii. It is the current casual-gaming look
because it survives being small -- the depth reads at 17px where fine detail
does not.

Two constraints that shaped every icon below:

  17px   the nav renders these tiny. Anything thinner than ~6 units of a
         100-unit viewBox disappears, so there is no hairline anywhere.
  currentColor is NOT used. These are full-colour marks; the nav tints nothing.
"""

# Shared gradient defs, emitted once per SVG that needs them.
def _grad(gid, top, bottom):
    return (f'<linearGradient id="{gid}" x1="0" y1="0" x2="0" y2="1">'
            f'<stop offset="0" stop-color="{top}"/>'
            f'<stop offset="1" stop-color="{bottom}"/></linearGradient>')


ICONS = {
    # X and O as chunky pieces with a base, tilted so they read as objects
    # rather than as typography.
    "xo": f'''<svg viewBox="0 0 100 100" fill="none">
  <defs>{_grad("xoA", "#FF8A5B", "#F2622E")}{_grad("xoB", "#4FE0CB", "#22B0A0")}</defs>
  <g transform="rotate(-8 34 36)">
    <rect x="12" y="30" width="44" height="13" rx="6.5" fill="#C94A1E" transform="rotate(45 34 36)"/>
    <rect x="12" y="30" width="44" height="13" rx="6.5" fill="#C94A1E" transform="rotate(-45 34 36)"/>
    <rect x="12" y="27" width="44" height="13" rx="6.5" fill="url(#xoA)" transform="rotate(45 34 33)"/>
    <rect x="12" y="27" width="44" height="13" rx="6.5" fill="url(#xoA)" transform="rotate(-45 34 33)"/>
  </g>
  <circle cx="69" cy="69" r="23" fill="#159487"/>
  <circle cx="69" cy="66" r="23" fill="url(#xoB)"/>
  <circle cx="69" cy="66" r="11" fill="#EEF0FF"/>
</svg>''',

    # A board with a disc mid-drop -- the moment, not the object.
    "connect-four": f'''<svg viewBox="0 0 100 100" fill="none">
  <defs>{_grad("c4F", "#5E61B8", "#3D3F86")}{_grad("c4R", "#FF6B6B", "#E8434F")}{_grad("c4Y", "#FFD84D", "#F5B518")}</defs>
  <rect x="10" y="30" width="80" height="62" rx="14" fill="#2F3170"/>
  <rect x="10" y="26" width="80" height="62" rx="14" fill="url(#c4F)"/>
  <circle cx="31" cy="70" r="10" fill="url(#c4R)"/>
  <circle cx="55" cy="70" r="10" fill="url(#c4Y)"/>
  <circle cx="79" cy="70" r="10" fill="#2A2C63" opacity=".45"/>
  <circle cx="31" cy="45" r="10" fill="#2A2C63" opacity=".45"/>
  <circle cx="55" cy="45" r="10" fill="url(#c4R)"/>
  <circle cx="79" cy="45" r="10" fill="#2A2C63" opacity=".45"/>
  <circle cx="79" cy="14" r="11" fill="#C9333E"/>
  <circle cx="79" cy="11" r="11" fill="url(#c4Y)"/>
</svg>''',

    # A body with a head and a highlight, curving rather than a row of squares.
    "snake": f'''<svg viewBox="0 0 100 100" fill="none">
  <defs>{_grad("snB", "#5AE3CF", "#22B0A0")}{_grad("snA", "#FF8A5B", "#F2622E")}</defs>
  <path d="M14 72 h26 a12 12 0 0 0 12-12 v-8 a12 12 0 0 1 12-12 h14"
        stroke="#159487" stroke-width="21" stroke-linecap="round" fill="none"/>
  <path d="M14 68 h26 a12 12 0 0 0 12-12 v-8 a12 12 0 0 1 12-12 h14"
        stroke="url(#snB)" stroke-width="21" stroke-linecap="round" fill="none"/>
  <circle cx="80" cy="36" r="4.5" fill="#0E5F57"/>
  <circle cx="26" cy="24" r="12" fill="#C94A1E"/>
  <circle cx="26" cy="21" r="12" fill="url(#snA)"/>
  <ellipse cx="22" cy="17" rx="4" ry="3" fill="#fff" opacity=".55"/>
</svg>''',

    # Stacked tiles with real depth; the front tile is the one that matters.
    "2048": f'''<svg viewBox="0 0 100 100" fill="none">
  <defs>{_grad("tfA", "#FFD98A", "#FFB627")}{_grad("tfB", "#FFA46B", "#F2622E")}</defs>
  <rect x="8" y="12" width="38" height="38" rx="11" fill="#C9CFF5"/>
  <rect x="8" y="8" width="38" height="38" rx="11" fill="#DDE2FF"/>
  <rect x="54" y="12" width="38" height="38" rx="11" fill="#C9822F"/>
  <rect x="54" y="8" width="38" height="38" rx="11" fill="url(#tfA)"/>
  <rect x="8" y="58" width="38" height="38" rx="11" fill="#159487"/>
  <rect x="8" y="54" width="38" height="38" rx="11" fill="#2BC4AE"/>
  <rect x="54" y="58" width="38" height="38" rx="11" fill="#C94A1E"/>
  <rect x="54" y="54" width="38" height="38" rx="11" fill="url(#tfB)"/>
</svg>''',

    # A grid that reads as a grid at 17px: heavy box rules, three bold digits.
    "sudoku": f'''<svg viewBox="0 0 100 100" fill="none">
  <defs>{_grad("sdF", "#5AE3CF", "#22B0A0")}</defs>
  <rect x="8" y="14" width="84" height="84" rx="14" fill="#159487"/>
  <rect x="8" y="10" width="84" height="84" rx="14" fill="url(#sdF)"/>
  <rect x="16" y="18" width="68" height="68" rx="8" fill="#F7F8FF"/>
  <g stroke="#B9C0EE" stroke-width="4" stroke-linecap="round">
    <line x1="38.7" y1="20" x2="38.7" y2="84"/><line x1="61.3" y1="20" x2="61.3" y2="84"/>
    <line x1="18" y1="40.7" x2="82" y2="40.7"/><line x1="18" y1="63.3" x2="82" y2="63.3"/>
  </g>
  <g font-family="system-ui,-apple-system,sans-serif" font-weight="800" font-size="19" text-anchor="middle">
    <text x="27" y="36" fill="#F2622E">5</text>
    <text x="50" y="59" fill="#FFB627">3</text>
    <text x="73" y="81" fill="#4A4C9B">7</text>
  </g>
</svg>''',

    # Two cards, one mid-flip -- perspective is what makes it read as Memory
    # rather than as three rectangles.
    "memory": f'''<svg viewBox="0 0 100 100" fill="none">
  <defs>{_grad("mmA", "#6E71C9", "#4A4C9B")}{_grad("mmB", "#FF8A5B", "#F2622E")}</defs>
  <g transform="rotate(-10 30 54)">
    <rect x="10" y="26" width="40" height="56" rx="10" fill="#33356F"/>
    <rect x="10" y="22" width="40" height="56" rx="10" fill="url(#mmA)"/>
    <circle cx="30" cy="50" r="9" fill="#8F92E0" opacity=".75"/>
  </g>
  <g transform="rotate(9 66 50)">
    <rect x="50" y="20" width="40" height="56" rx="10" fill="#C94A1E"/>
    <rect x="50" y="16" width="40" height="56" rx="10" fill="url(#mmB)"/>
    <path d="M62 44 l6 7 12 -14" stroke="#fff" stroke-width="7"
          stroke-linecap="round" stroke-linejoin="round" fill="none"/>
  </g>
</svg>''',

    # The daily is a calendar page, not a game -- it should not look like one.
    "daily": f'''<svg viewBox="0 0 100 100" fill="none">
  <defs>{_grad("dyF", "#FFD98A", "#FFB627")}</defs>
  <rect x="12" y="22" width="76" height="72" rx="14" fill="#C9822F"/>
  <rect x="12" y="18" width="76" height="72" rx="14" fill="url(#dyF)"/>
  <rect x="20" y="38" width="60" height="44" rx="8" fill="#FFF8EC"/>
  <rect x="28" y="8" width="11" height="22" rx="5.5" fill="#8A5A12"/>
  <rect x="61" y="8" width="11" height="22" rx="5.5" fill="#8A5A12"/>
  <g font-family="system-ui,-apple-system,sans-serif" font-weight="800" font-size="26" text-anchor="middle">
    <text x="50" y="72" fill="#F2622E">1</text>
  </g>
</svg>''',
}

# Nav order. The daily leads because it is the only thing with a reason to come
# back tomorrow.
NAV = [
    ("/daily", "Daily", "Daily", "daily"),
    ("/xo", "Tic-Tac-Toe", "XO", "xo"),
    ("/connect-four", "Connect Four", "4-Row", "connect-four"),
    ("/snake", "Snake", "Snake", "snake"),
    ("/2048", "2048", "2048", "2048"),
    ("/sudoku", "Sudoku", "Sudoku", "sudoku"),
    ("/memory", "Memory", "Memory", "memory"),
]
