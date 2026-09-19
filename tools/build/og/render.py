#!/usr/bin/env python3
"""Render the Open Graph image from HTML, at exactly 1200x630.

    python3 tools/build/og/render.py

Rendered in a browser rather than drawn with an image library, because the
previous stdlib-only Go generator had no font support -- the title came out as
a black bar, and the image advertised three games when the site had six. A
preview image is the first thing anyone sees of a shared link, and it was both
stale and broken.

Using the browser we already drive for tests means real typography and the
site's own palette, with no new dependency.
"""
import pathlib, sys

sys.path.insert(0, "tools/test")
from cdp import Browser                                        # noqa: E402

HERE = pathlib.Path(__file__).parent
OUT = pathlib.Path("apps/publisher/og.png")


def main():
    b = Browser(1200, 630)
    try:
        b.goto("file://" + str((HERE / "template.html").resolve()))
        # Fonts and the SVG tiles need a beat to settle; a screenshot taken
        # mid-layout is a subtly wrong image nobody notices until it is public.
        b.eval("document.fonts ? document.fonts.ready.then(function(){return 1}) : 1")
        b.shot(str(OUT))
    finally:
        b.close()
    size = OUT.stat().st_size
    print(f"wrote {OUT} ({size:,} bytes)")
    if size < 10_000:
        print("suspiciously small -- did it render?", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
