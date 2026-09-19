#!/usr/bin/env python3
"""Production sweep. Run before sending anyone to the site.

    make check-prod

site_check.py tests the BUILD. This tests what a visitor actually gets: the
CDN, the ad server, the link previews, the real HTML. Those are different
things, and the gap between them is where the CDN 403/404 bug lived.

The link-preview checks matter more than they look. A Reddit or social post
whose preview image 404s performs dramatically worse, and the failure is
invisible from the site itself -- nothing is broken, the link just looks dead.
"""
import json, subprocess, sys, time

sys.path.insert(0, "tools/test")
from cdp import Browser                                        # noqa: E402

SITE = "https://xoxoxo.live"
PAGES = ["/", "/daily/", "/xo/", "/connect-four/", "/snake/", "/2048/",
         "/sudoku/", "/memory/", "/privacy/"]

ok, fail = [], []


def check(name, cond, detail=""):
    (ok if cond else fail).append(name)
    print(f"  {'PASS' if cond else 'FAIL'}  {name}{('  -- ' + detail) if detail and not cond else ''}")


# curl rather than urllib. This machine's Python has no CA bundle -- every
# HTTPS request fails with CERTIFICATE_VERIFY_FAILED while curl and Chrome are
# both fine. Shelling out avoids adding certifi as a dependency for a script
# whose entire job is to check that a public site works.
UA = ("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "
      "AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile Safari/604.1")


def head(url):
    r = subprocess.run(["curl", "-sS", "-o", "/dev/null", "-w", "%{http_code}",
                        "-I", "-m", "20", "-A", UA, url],
                       capture_output=True, text=True)
    try:
        return int(r.stdout.strip() or 0), ""
    except ValueError:
        return 0, r.stderr.strip()


def get(url):
    r = subprocess.run(["curl", "-sS", "-m", "20", "-A", UA, url],
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError(r.stderr.strip() or f"curl exit {r.returncode}")
    return r.stdout


def post_json(url, payload):
    r = subprocess.run(["curl", "-sS", "-m", "20", "-A", UA,
                        "-H", "Content-Type: application/json",
                        "-d", json.dumps(payload), url],
                       capture_output=True, text=True)
    if r.returncode != 0:
        raise RuntimeError(r.stderr.strip() or f"curl exit {r.returncode}")
    return json.loads(r.stdout)


def main():
    print("serving")
    for p in PAGES:
        code, _ = head(SITE + p)
        check(f"{p} serves 200", code == 200, str(code))

    print("\nsupply chain and crawlers")
    for p, must in [("/ads.txt", "xoxoxo-1"), ("/sellers.json", "xoxoxo-1"),
                    ("/app-ads.txt", "placeholder"), ("/robots.txt", "Sitemap"),
                    ("/sitemap.xml", "/daily")]:
        try:
            body = get(SITE + p)
            check(f"{p} present and correct", must in body, f"missing {must!r}")
        except Exception as e:
            check(f"{p} present", False, str(e))

    print("\nad server")
    try:
        res = post_json(SITE + "/ad/request",
                        {"placement_id": "game_sidebar", "game": "xo",
                         "session_id": "prod-check", "device_type": "mobile",
                         "w": 300, "h": 250, "env": "lab"})
        check("POST /ad/request returns an ad", bool(res.get("ad")), json.dumps(res)[:120])
    except Exception as e:
        check("POST /ad/request", False, str(e))

    # Frequency capping, counted against the LIVE endpoint.
    #
    # This exists because the cap shipped inert: FrequencyState was wired only
    # in the local branch of main(), so it was nil in Lambda and every cap
    # silently did nothing. Unit tests passed -- the gap was in wiring, not
    # logic, and only counting real impressions finds that.
    print("\ntraffic classification (live)")
    # The other half of the story: automation must NOT be billed. These are the
    # cases the classifier exists for, checked against production rather than
    # asserted in a unit test.
    for label, ua in [
        ("headless browser", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 "
                             "HeadlessChrome/120.0.0.0 Safari/537.36"),
        ("declared crawler", "Mozilla/5.0 (compatible; Googlebot/2.1; "
                             "+http://www.google.com/bot.html)"),
    ]:
        r = subprocess.run(["curl", "-sS", "-m", "20", "-A", ua,
                            "-H", "Content-Type: application/json",
                            "-d", json.dumps({"placement_id": "game_sidebar", "game": "xo",
                                              "session_id": "classify-" + label.split()[0],
                                              "device_type": "desktop", "w": 300, "h": 250,
                                              "env": "production"}),
                            SITE + "/ad/request"], capture_output=True, text=True)
        try:
            body = json.loads(r.stdout)
            check(f"a {label} is not served a billable ad", bool(body.get("no_ad")),
                  r.stdout[:120])
        except Exception as e:
            check(f"a {label} is classified", False, str(e))

    print("\nfrequency capping (live)")
    import uuid
    sid = "prodcheck-" + uuid.uuid4().hex[:10]
    served = []
    for _ in range(6):
        try:
            r = post_json(SITE + "/ad/request",
                          {"placement_id": "game_sidebar", "game": "xo",
                           "session_id": sid, "device_type": "desktop",
                           "w": 300, "h": 250, "env": "production"})
            ad = r.get("ad") or {}
            served.append(ad.get("line_item_id") or "no_ad")
        except Exception as e:
            served.append("error:" + str(e)[:40])
    paid = [s for s in served if s.startswith("li-") and "house" not in s]
    check("a paying line item stops at its cap within one session",
          len(paid) <= 3, f"served {len(paid)} times: {served}")
    check("the slot still fills after the cap is reached",
          served[-1] != "no_ad", str(served))

    b = Browser(390, 664)
    try:
        print("\nlink previews")
        # A social post whose preview 404s performs far worse, and nothing on
        # the site looks broken -- so it is invisible unless checked here.
        for p in ["/", "/daily/", "/sudoku/"]:
            b.goto(SITE + p)
            meta = json.loads(b.eval(
                "(function(){var g=function(n){var e="
                "document.querySelector('meta[property=\"'+n+'\"]')||"
                "document.querySelector('meta[name=\"'+n+'\"]');"
                "return e?e.content:''};"
                "return JSON.stringify({title:g('og:title'),desc:g('og:description'),"
                "img:g('og:image'),card:g('twitter:card'),"
                "canonical:(document.querySelector('link[rel=canonical]')||{}).href||''});})()"))
            check(f"{p} has og:title and description",
                  bool(meta["title"]) and bool(meta["desc"]), str(meta))
            check(f"{p} canonical points at the site",
                  meta["canonical"].startswith(SITE), meta["canonical"])
            if meta["img"]:
                code, _ = head(meta["img"])
                check(f"{p} og:image actually resolves", code == 200,
                      f"{meta['img']} -> {code}")
            else:
                check(f"{p} has an og:image", False, "none")

        print("\nplayable")
        plays = [
            ("/xo/", "#board > *", None, "document.querySelectorAll('#board .mark').length >= 1"),
            ("/connect-four/", "#c4-cols > *", None,
             "document.querySelectorAll('#c4-grid .disc').length >= 1"),
            ("/2048/", None, ("ArrowLeft", 37),
             "document.querySelectorAll('#g2048 .tf-tile').length >= 2"),
            ("/sudoku/", "#sudoku .sd-cell:not(.given)", None,
             "document.querySelectorAll('#sudoku .sd-cell').length === 81"),
            ("/memory/", "#memory .mm-card", None,
             "document.querySelectorAll('#memory .mm-card.up, #memory .mm-card.done').length >= 1"),
        ]
        for path, click, key, expect in plays:
            b.goto(SITE + path)
            time.sleep(0.5)
            if click:
                try:
                    b.click_sel(click)
                except AssertionError as e:
                    check(f"{path} responds to input", False, str(e))
                    continue
            if key:
                b.key(key[0], vk=key[1])
            time.sleep(0.7)
            check(f"{path} responds to input", bool(b.eval(expect)))
            errs = b.eval("JSON.stringify(window.__errs||[])")
            check(f"{path} logs no console errors", errs == "[]", errs)

        print("\ndaily")
        b.goto(SITE + "/daily/")
        b.eval("localStorage.clear(); 1")
        b.goto(SITE + "/daily/")
        time.sleep(1.0)
        d = json.loads(b.eval(
            "JSON.stringify({cells:document.querySelectorAll('#daily-board .sd-cell').length,"
            "givens:document.querySelectorAll('#daily-board .sd-cell.given').length,"
            "num:document.getElementById('dy-number').textContent,"
            "errs:(window.__errs||[]).slice(0,2)})"))
        check("daily renders a full board", d["cells"] == 81, str(d))
        check("daily has givens", 25 <= d["givens"] <= 50, str(d["givens"]))
        check("daily shows a puzzle number", d["num"].startswith("#"), d["num"])
        check("daily logs no console errors", d["errs"] == [], str(d["errs"]))

        print("\nads render for a visitor")
        # data-filled is a flag the ad tag sets on ITSELF. It is the tag
        # reporting its own success, and it was the whole assertion here -- so
        # this suite said ads rendered while nobody had checked that a single
        # pixel existed. Litan looked at the page and saw nothing, and the
        # suite had no way to disagree with him.
        #
        # What a person would recognise: a box with area, actually visible, with
        # something inside it.
        for p in ["/xo/", "/snake/"]:
            b.goto(SITE + p)
            time.sleep(2.5)
            m = json.loads(b.eval("""JSON.stringify((function(){
              var s=document.getElementById('ad-slot');
              if(!s) return {found:false};
              var r=s.getBoundingClientRect(), cs=getComputedStyle(s);
              return {found:true, filled:s.dataset.filled,
                w:Math.round(r.width), h:Math.round(r.height),
                visible: cs.display!=='none' && cs.visibility!=='hidden' && cs.opacity!=='0',
                children:s.children.length,
                top:Math.round(r.top+window.scrollY), fold:window.innerHeight};})())"""))
            check(f"{p} slot exists", m.get("found"), str(m))
            check(f"{p} the tag reports it filled", m.get("filled") == "1", str(m))
            check(f"{p} the slot has real area", m.get("w", 0) >= 100 and m.get("h", 0) >= 50,
                  f"{m.get('w')}x{m.get('h')}")
            check(f"{p} the slot is actually visible", m.get("visible"), str(m))
            check(f"{p} something was put inside it", m.get("children", 0) > 0, str(m))
            # Reported, not asserted. Below the fold is a deliberate placement
            # choice -- the board comes first -- and it costs viewability. It
            # should be visible on the page, not invisible in a log.
            where = "above the fold" if m.get("top", 0) < m.get("fold", 0) \
                else f"{m['top'] - m['fold']}px below the fold"
            print(f"        {p} sits {where} at {m.get('fold')}px tall")
    finally:
        b.close()

    print(f"\n{len(ok)} passed, {len(fail)} failed")
    for f in fail:
        print(f"  FAILED: {f}")
    return 1 if fail else 0


if __name__ == "__main__":
    sys.exit(main())
