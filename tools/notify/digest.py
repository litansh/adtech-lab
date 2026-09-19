#!/usr/bin/env python3
"""Assemble one weekly message out of what the agents actually found.

    python3 tools/notify/digest.py --product /tmp/product.txt \
        --supplychain /tmp/chain.txt --signals product/signals.json

WHY THIS EXISTS

The agents that need no traffic -- Product, Horizon, Supply chain -- have been
running correctly and writing to a CI log nobody opens. From outside, an agent
whose findings never leave the runner is indistinguishable from one that is not
running, which is the confusion this project has spent its whole life removing.

WHY IT IS WEEKLY, AND ONE MESSAGE

Because the alternative is four messages a day and then a muted channel. These
findings move slowly on purpose: a product benchmark that changed daily would be
measuring noise. One message, once a week, is the honest cadence for what these
agents actually know.

WHAT IT WILL NOT DO

It will not invent output for the four agents that need traffic. Yield,
Integrity, FinOps and Demand have nothing to say until someone plays a game, and
the digest says so by name rather than quietly omitting them -- a fleet that
looks smaller than it is teaches you to expect less than you should.
"""
import argparse, json, pathlib, re, sys

LIMIT = 2600  # telegram.py trims above ~2800; leave room for the header


def read(path):
    if not path:
        return ""
    try:
        return pathlib.Path(path).read_text()
    except OSError:
        return ""


def product_lines(text):
    """The three lines of the benchmark worth a phone: the gap, the next day of
    work, and the direction. The ranked table is a terminal thing."""
    out = []
    for key, label in (("GAP", "Gap"), ("DO NEXT", "Do next"), ("DIRECT.", "Direction")):
        m = re.search(rf"^{re.escape(key)}\s+(.+)$", text, re.M)
        if m:
            out.append(f"{label}: {m.group(1).strip()}")
    return out


def horizon_lines(path):
    """Read the last scan rather than re-running it: the scan is Horizon's job
    and this is a messenger. A stale file is reported as stale."""
    try:
        d = json.loads(pathlib.Path(path).read_text())
    except (OSError, ValueError):
        return ["Horizon: no scan on file yet"]

    out = [f"Scanned {d.get('scanned_at', '?')} — {d.get('items_read', 0)} items"]
    for m in (d.get("mechanics") or [])[:3]:
        ship = "we ship it" if m.get("we_ship_it") else "WE SHIP NONE"
        out.append(f"  {m['name']}: {m['points']} pts, {ship}")
    gaps = d.get("gaps") or []
    if gaps:
        out.append("  gap: " + ", ".join(g["name"] for g in gaps))
    return out


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--product")
    p.add_argument("--supplychain")
    p.add_argument("--signals", default="product/signals.json")
    p.add_argument("--traffic", type=int, default=0,
                   help="event files seen; 0 means the inward agents are idle")
    a = p.parse_args()

    parts = []

    prod = product_lines(read(a.product))
    if prod:
        parts.append("PRODUCT\n" + "\n".join(prod))

    chain = read(a.supplychain)
    m = re.search(r"^result:\s*(\w+)", chain, re.M)
    if m:
        verdict = m.group(1)
        note = ("our ads.txt and sellers.json agree"
                if verdict == "OK" else "a buyer would refuse this inventory")
        parts.append(f"SUPPLY CHAIN\n{verdict} — {note}")

    parts.append("HORIZON\n" + "\n".join(horizon_lines(a.signals)))

    # Named, not omitted. A fleet that looks smaller than it is teaches you to
    # expect less from it than you should.
    if a.traffic <= 0:
        parts.append("WAITING FOR TRAFFIC\nYield, Integrity, FinOps and Demand "
                     "have nothing to report until someone plays a game. That is "
                     "correct, not broken.")

    body = "\n\n".join(parts)
    if len(body) > LIMIT:
        body = body[:LIMIT] + "\n… (full output in the run log)"
    print(body)
    return 0


if __name__ == "__main__":
    sys.exit(main())
