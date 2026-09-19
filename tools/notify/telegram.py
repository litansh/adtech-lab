#!/usr/bin/env python3
"""Post a message to a Telegram chat.

    TELEGRAM_BOT_TOKEN=... TELEGRAM_CHAT_ID=... \
      python3 tools/notify/telegram.py --title "Yield agent" --body-file /tmp/x.md --url https://...

Outbound only, on purpose.

The agents needed somewhere their findings actually reach a person. A nightly
report committed to the repository is a log nobody opens; a message on a phone
is read. So: agents post here.

What this deliberately does NOT do is accept commands. A bot that can be told
"deploy" or "approve" is a webhook on the public internet that triggers
production changes, authenticated by a token in a URL. The convenience is small
-- GitHub's mobile app already approves a pull request in two taps -- and the
attack surface is a new front door.

So every message links to the pull request or issue, and authority stays where
it already is. See docs/telegram.md for the inbound design and what it would
require if we ever decide the convenience is worth it.
"""
import argparse, json, os, re, subprocess, sys, urllib.parse

API = "https://api.telegram.org/bot{token}/sendMessage"


def send(token, chat_id, text, url=None):
    # curl rather than urllib: this machine's Python has no CA bundle, and a
    # notifier that fails on TLS is worse than no notifier.
    payload = {
        "chat_id": chat_id,
        "text": text,
        "parse_mode": "HTML",
        "disable_web_page_preview": True,
    }
    if url:
        payload["reply_markup"] = json.dumps({
            "inline_keyboard": [[{"text": "Open on GitHub", "url": url}]]
        })
    r = subprocess.run(
        ["curl", "-sS", "-m", "20", "-X", "POST",
         API.format(token=token),
         "-H", "Content-Type: application/x-www-form-urlencoded",
         "-d", urllib.parse.urlencode(payload)],
        capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError(r.stderr.strip())
    body = json.loads(r.stdout)
    if not body.get("ok"):
        raise RuntimeError(body.get("description", r.stdout[:200]))
    return body


URL_RE = re.compile(r"(https?://[^\s<>\"]+)")


def linkify(escaped):
    """Turn bare URLs into anchors. Runs AFTER escaping, so nothing in the
    body can inject markup -- the text is already inert when it gets here."""
    return URL_RE.sub(lambda m: f'<a href="{m.group(1)}">{m.group(1)}</a>', escaped)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--title", required=True)
    p.add_argument("--body")
    p.add_argument("--body-file")
    p.add_argument("--url", help="a pull request or issue to link to")
    p.add_argument("--plain", action="store_true",
                   help="render as prose with tappable links, not a <pre> block")
    p.add_argument("--max", type=int, default=2800,
                   help="Telegram rejects messages over ~4096 characters")
    a = p.parse_args()

    token = os.environ.get("TELEGRAM_BOT_TOKEN")
    chat = os.environ.get("TELEGRAM_CHAT_ID")
    if not token or not chat:
        # Not configured is not an error. The agents must run whether or not
        # anyone is listening.
        print("telegram: not configured (TELEGRAM_BOT_TOKEN / TELEGRAM_CHAT_ID unset)")
        return 0

    body = a.body or ""
    if a.body_file:
        with open(a.body_file) as f:
            body = f.read()

    def esc(s):
        return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")

    text = f"<b>{esc(a.title)}</b>"
    if body.strip():
        trimmed = body.strip()
        if len(trimmed) > a.max:
            trimmed = trimmed[:a.max] + "\n… (truncated — full detail on GitHub)"
        if a.plain:
            # Prose, with URLs turned into taps. <pre> is right for a tool's
            # tabular output and wrong for a briefing: it renders in a
            # monospace block, wraps badly on a phone, and -- the part that
            # actually matters -- makes every link untappable.
            text += "\n\n" + linkify(esc(trimmed))
        else:
            text += f"\n<pre>{esc(trimmed)}</pre>"

    try:
        send(token, chat, text, a.url)
        print("telegram: sent")
    except Exception as e:
        # A failed notification must never fail the job that produced the
        # finding. The finding is the valuable part.
        print(f"telegram: send failed ({e})", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
