#!/usr/bin/env python3
"""
Content-hash the static assets and rewrite the HTML that references them.

Why this exists: the deploy set `max-age=31536000, immutable` on files with
STABLE names. Immutable caching on a stable filename means a visitor who loaded
the old file never gets the new one -- for a year. The fix is not a shorter TTL,
it is filenames that change when the content changes.

This also makes the docs true. README and cost-model.md both claim the assets
are content-hashed, and until now they were not; the Model A caching assumption
in the cost model depends on it.

Output goes to a build/ directory so the source tree stays clean and readable.
"""
import hashlib
import pathlib
import re
import shutil
import sys

SRC = pathlib.Path("apps/publisher")
OUT = pathlib.Path("apps/publisher/build")
ASSETS = ["styles.css", "game-room.js", "snake.js", "g2048.js", "sudoku.js", "memory.js", "daily.js", "records.js", "today.js", "adlab.js", "adtag.js"]


def main() -> int:
    if not SRC.exists():
        print(f"no {SRC}", file=sys.stderr)
        return 1

    if OUT.exists():
        shutil.rmtree(OUT)
    (OUT / "static").mkdir(parents=True)

    # 1. hash each asset and copy it to its hashed name
    mapping = {}
    for name in ASSETS:
        src = SRC / "static" / name
        if not src.exists():
            print(f"missing asset: {src}", file=sys.stderr)
            return 1
        digest = hashlib.sha256(src.read_bytes()).hexdigest()[:10]
        stem, ext = name.rsplit(".", 1)
        hashed = f"{stem}.{digest}.{ext}"
        shutil.copy2(src, OUT / "static" / hashed)
        mapping[f"/static/{name}"] = f"/static/{hashed}"

    # 2. copy everything else, rewriting asset references in HTML
    for path in SRC.rglob("*"):
        if not path.is_file() or "build" in path.parts:
            continue
        rel = path.relative_to(SRC)
        if rel.parts[0] == "static":
            continue                     # handled above
        if path.suffix == ".md":
            continue
        dest = OUT / rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        if path.suffix == ".html":
            text = path.read_text()
            for old, new in mapping.items():
                text = text.replace(old, new)
            # a reference we failed to rewrite would be cached forever as a 404
            leftover = re.findall(r'/static/[a-z-]+\.(?:css|js)', text)
            if leftover:
                print(f"unrewritten reference in {rel}: {leftover}", file=sys.stderr)
                return 1
            dest.write_text(text)
        else:
            shutil.copy2(path, dest)

    for old, new in mapping.items():
        print(f"  {old:28} -> {new}")
    print(f"\nbuilt {OUT}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
