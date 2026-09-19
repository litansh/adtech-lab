#!/usr/bin/env python3
"""Publisher regression test. Run after every game change.

    make test-site

Checks, per the contract in games.json:
  structure  every page 200s, has the full nav, every nav link resolves
  boot       each game's required DOM ids exist and the module bound to them
  play       real key/click input changes the board -- not just "it rendered"
  layout     the board is above the fold at 763px, the measurement that caught
             Connect Four and 2048 shipping half-hidden on a laptop
  console    no page logs an error or throws

It is deliberately not a visual regression suite. It answers "is it still
playable and still navigable", which is the failure mode we actually keep
hitting, and it runs in about fifteen seconds.
"""
import http.server, json, os, pathlib, re, socket, socketserver, subprocess, sys, threading, urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools" / "test"))
from cdp import Browser                                    # noqa: E402

BUILD = ROOT / "apps" / "publisher" / "build"
SPEC = json.loads((ROOT / "tools" / "test" / "games.json").read_text())
ok, fail = [], []


CUR = {"vp": ""}


def check(name, cond, detail=""):
    name = f"[{CUR['vp']}] {name}"
    (ok if cond else fail).append(name)
    print(f"  {'PASS' if cond else 'FAIL'}  {name}{('  -- ' + detail) if detail and not cond else ''}")


def serve(directory):
    class H(http.server.SimpleHTTPRequestHandler):
        def __init__(self, *a, **k):
            super().__init__(*a, directory=str(directory), **k)

        def log_message(self, *a):
            pass

        def handle_one_request(self):
            # A browser closing a keep-alive connection is normal, not an error
            # worth a traceback in the middle of the report.
            try:
                super().handle_one_request()
            except (ConnectionResetError, BrokenPipeError):
                self.close_connection = True
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    # Threaded, not single-threaded. The page holds keep-alive connections and
    # now defers its ad fetch until the slot nears the viewport, so a
    # one-request-at-a-time server deadlocks: the deferred request waits behind
    # a connection the browser has not closed, and the page never finishes.
    httpd = http.server.ThreadingHTTPServer(("127.0.0.1", port), H)
    httpd.daemon_threads = True
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return httpd, f"http://127.0.0.1:{port}"


def main():
    # Always rebuild. A test that can silently pass against a stale build is
    # worse than no test -- it reports the last known-good state as current.
    subprocess.run([sys.executable, str(ROOT / "tools/build/hash-assets.py")],
                   cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
    httpd, base = serve(BUILD)
    try:
        for vp in SPEC["viewports"]:
            run(base, vp)
    finally:
        httpd.shutdown()

    print(f"\n{len(ok)} passed, {len(fail)} failed")
    for f in fail:
        print(f"  FAILED: {f}")
    return 1 if fail else 0


def run(base, vp):
    print(f"\n=== {vp['name']} {vp['w']}x{vp['h']} ===")
    b = Browser(vp["w"], vp["h"])
    CUR["vp"] = vp["name"]
    try:
        print("\nstructure")
        for p in SPEC["pages"]:
            code = urllib.request.urlopen(base + p, timeout=10).status
            check(f"{p} serves 200", code == 200, str(code))

        for p in SPEC["pages"]:
            b.goto(base + p)
            hrefs = b.eval("JSON.stringify([...document.querySelectorAll('.tabs .tab')]"
                           ".map(a=>[a.getAttribute('href'),(a.querySelector('.lbl')||a).textContent.trim()]))")
            got = json.loads(hrefs)
            want = [[n["href"], n["label"]] for n in SPEC["nav"]]
            check(f"{p} nav lists every game", got == want, f"got {got}")
            ov = json.loads(b.eval(
                "(function(){var t=document.querySelector('.tabs');"
                "return JSON.stringify([t.scrollWidth,t.clientWidth,"
                "document.documentElement.scrollWidth,innerWidth]);})()"))
            check(f"{p} nav fits without clipping", ov[0] <= ov[1] + 1,
                  f"strip {ov[0]}px in {ov[1]}px")
            check(f"{p} no horizontal page scroll", ov[2] <= ov[3] + 1,
                  f"page {ov[2]}px in {ov[3]}px")

        print("\nnav links resolve")
        for n in SPEC["nav"]:
            code = urllib.request.urlopen(base + n["href"] + "/", timeout=10).status
            check(f"{n['href']} resolves", code == 200, str(code))

        for g in SPEC["games"]:
            print(f"\n{g['name']}")
            b.goto(base + g["path"])
            missing = [s for s in g["requires"]
                       if not b.eval(f"!!document.querySelector({json.dumps(s)})")]
            check("required elements present", not missing, f"missing {missing}")

            board = json.dumps(g["board"])
            if not b.eval(f"!!document.querySelector({board})"):
                check("board present", False, g["board"])
                continue
            check("board rendered by its module",
                  b.eval(f"document.querySelector({board}).innerHTML.length") > 0)

            for step in g["play"]:
                if "key" in step:
                    b.key(step["key"], vk=step.get("vk"))
                elif "click" in step:
                    b.click_sel(step["click"])
                elif "wait" in step:
                    b.eval(f"new Promise(r=>setTimeout(r,{int(step['wait']*1000)}))")
            check("responds to input", bool(b.eval(g["expect"])), "expect was false")

            m = json.loads(b.eval(
                "(function(){var r=document.querySelector(" + board +
                ").getBoundingClientRect();return JSON.stringify({top:Math.round(r.top),"
                "bottom:Math.round(r.bottom),vh:innerHeight});})()"))
            check(f"board above the fold ({m['bottom']}/{m['vh']}px)",
                  m["bottom"] <= m["vh"], f"{m['bottom'] - m['vh']}px below")

            errs = b.eval("JSON.stringify(window.__errs||[])")
            check("no console errors", errs == "[]", errs)

        print("\nad slot fills")
        # The regression this guards: an unfilled slot that is display:none is
        # never observed, so the lazy request never fires and the slot never
        # fills. It has to be in the layout tree to be fillable.
        b.goto(base + "/snake/")
        vis = json.loads(b.eval(
            "(function(){var s=document.getElementById('ad-slot');"
            "var cs=getComputedStyle(s);"
            "return JSON.stringify({display:cs.display,rects:s.getClientRects().length});})()"))
        check("an empty ad slot is still in the layout tree",
              vis["display"] != "none" and vis["rects"] > 0, str(vis))
        # And it takes no visible space until it fills.
        h = b.eval("document.getElementById('ad-slot').getBoundingClientRect().height")
        check("an empty ad slot takes no space", h == 0, f"height {h}")

        print("\nad placement")
        b.goto(base + "/snake/")
        check("end-of-game slot is hidden before the game ends",
              b.eval("document.getElementById('endgame').hidden"))
        # Force the "on" arm, then end a game.
        b.eval("try{sessionStorage.setItem('adlab_endgame_arm','on')}catch(e){};"
               "AdLab.track('game_end',{game:'snake',outcome:'lose',score:10}); 1")
        check("end-of-game slot appears after game_end",
              b.eval("!document.getElementById('endgame').hidden"))
        check("the replay button stays above the new slot",
              b.eval("document.getElementById('sn-again').getBoundingClientRect().top"
                     " < document.getElementById('endgame').getBoundingClientRect().top"))
        b.goto(base + "/snake/")
        b.eval("try{sessionStorage.setItem('adlab_endgame_arm','off')}catch(e){};"
               "AdLab.track('game_end',{game:'snake',outcome:'lose',score:10}); 1")
        check("the off arm never reveals the slot",
              b.eval("document.getElementById('endgame').hidden"))
        b.goto(base + "/")
        check("the home page carries its own slot",
              b.eval("!!document.querySelector('[data-placement=\"home_below_games\"]')"))

        print("\npreview image")
        b.goto(base + "/")
        og = json.loads(b.eval(
            "(function(){var m=document.querySelector('meta[property=\"og:image\"]');"
            "return JSON.stringify({url:m?m.content:''});})()"))
        check("home page declares an og:image", bool(og["url"]), str(og))
        dims = json.loads(b.eval(
            "(function(){return new Promise(function(res){var i=new Image();"
            "i.onload=function(){res(JSON.stringify({w:i.naturalWidth,h:i.naturalHeight}))};"
            "i.onerror=function(){res(JSON.stringify({w:0,h:0}))};"
            "i.src='/og.png';});})()"))
        # 1200x630 is the size every major platform crops to. Anything else is
        # cropped unpredictably, which is worse than a smaller correct image.
        check("og.png is 1200x630", dims["w"] == 1200 and dims["h"] == 630, str(dims))

        print("\nsupply chain files")
        for path, must in [("/ads.txt", "xoxoxo.live, xoxoxo-1, DIRECT"),
                           ("/app-ads.txt", "placeholder.example.com"),
                           ("/sellers.json", "xoxoxo-1"),
                           ("/robots.txt", "Sitemap:")]:
            try:
                body = urllib.request.urlopen(base + path, timeout=10).read().decode()
                check(f"{path} is served and declares what it should", must in body,
                      f"missing {must!r}")
            except Exception as e:
                check(f"{path} is served", False, str(e))
        # sellers.json must parse: a buyer that cannot parse it treats the
        # inventory as unverifiable, which is the same as unauthorised.
        try:
            sj = json.loads(urllib.request.urlopen(base + "/sellers.json", timeout=10).read())
            check("sellers.json parses and has a seller",
                  len(sj.get("sellers", [])) > 0, "no sellers")
        except Exception as e:
            check("sellers.json parses", False, str(e))

        print("\n2048 level maths")
        b.goto(base + "/2048/")
        m = json.loads(b.eval(
            "JSON.stringify({"
            "min:[128,256,512].map(function(t){return G2048Math.minScoreFor(t)}),"
            "lv:[0,700,800,1800,4100].map(function(s){return G2048Math.levelFor(s)}),"
            "next:[0,1,7].map(function(l){return G2048Math.nextTarget(l)})})"))
        # Building tile T needs at least T x (log2(T) - 1): every merge that
        # builds it contributes its own value.
        check("milestone score thresholds are 768 / 1792 / 4096",
              m["min"] == [768, 1792, 4096], str(m["min"]))
        check("level rises only on crossing a threshold",
              m["lv"] == [0, 0, 1, 2, 3], str(m["lv"]))
        check("past the last milestone there is no next target",
              m["next"][2] is None and m["next"][0] == 128, str(m["next"]))

        print("\ndaily puzzle")
        b.goto(base + "/daily/")
        b.eval("localStorage.clear(); 1")
        b.goto(base + "/daily/")
        sig1 = b.eval("[...document.querySelectorAll('#daily-board .sd-cell')]"
                      ".map(function(c){return c.textContent||'.'}).join('')")
        b.goto(base + "/daily/")
        b.eval("localStorage.clear(); 1")
        b.goto(base + "/daily/")
        sig2 = b.eval("[...document.querySelectorAll('#daily-board .sd-cell')]"
                      ".map(function(c){return c.textContent||'.'}).join('')")
        check("the daily is identical for a fresh player", sig1 == sig2 and len(sig1) == 81)
        givens = len(sig1.replace(".", ""))
        check(f"the daily has a sensible number of givens ({givens})", 30 <= givens <= 45)
        seeds = json.loads(b.eval(
            "(function(){function h(s){var x=2166136261>>>0;for(var i=0;i<s.length;i++)"
            "{x^=s.charCodeAt(i);x=Math.imul(x,16777619)>>>0}return x>>>0}"
            "function r(sd){var a=sd>>>0;return function(){a=(a+0x6D2B79F5)>>>0;"
            "var t=Math.imul(a^(a>>>15),1|a);t=(t+Math.imul(t^(t>>>7),61|t))^t;"
            "return ((t^(t>>>14))>>>0)/4294967296}}"
            "var a=SudokuGen.generate(38,r(h('sudoku-2026-08-29'))).puzzle.join('');"
            "var a2=SudokuGen.generate(38,r(h('sudoku-2026-08-29'))).puzzle.join('');"
            "var c=SudokuGen.generate(38,r(h('sudoku-2026-08-30'))).puzzle.join('');"
            "return JSON.stringify({same:a===a2,diff:a!==c});})()"))
        check("the same date always generates the same puzzle", seeds["same"])
        check("a different date generates a different puzzle", seeds["diff"])

        print("\nsnake level maths")
        b.goto(base + "/snake/")
        m = json.loads(b.eval(
            "JSON.stringify({"
            "need:[1,2,3,4].map(function(n){return SnakeMath.applesForLevel(n)}),"
            "score:[1,2,5].map(function(n){return SnakeMath.scoreForApple(n)}),"
            "tick:[1,2,10].map(function(n){return Math.round(SnakeMath.tickForLevel(150,n))})})"))
        check("level 1 needs 3 apples, then 4, 5, 6",
              m["need"] == [3, 4, 5, 6], str(m["need"]))
        check("an apple is worth 10 x level", m["score"] == [10, 20, 50], str(m["score"]))
        check("speed rises with level and is floored",
              m["tick"][0] > m["tick"][1] > m["tick"][2] and m["tick"][2] >= 67,
              str(m["tick"]))

        print("\nrecords")
        b.goto(base + "/2048/")
        b.eval("localStorage.removeItem('adlab_records_v1');"
               "AdLab.track('game_end',{game:'2048',outcome:'win',score:2048});"
               "AdLab.track('game_end',{game:'xo',outcome:'win'}); 1")
        b.goto(base + "/")
        check("records table appears after finishing a game",
              b.eval("!document.getElementById('records').hidden"),
              "table stayed hidden after two finished games")
        rows = json.loads(b.eval(
            "JSON.stringify([...document.querySelectorAll('#records-body tr')]"
            ".map(r=>[...r.cells].map(c=>c.textContent)))"))
        check("records table lists both games played", len(rows) == 2, str(rows))
        check("best score is carried through", any("2048" in r for r in rows), str(rows))

        # A first-time visitor must not see an empty promise.
        b.eval("localStorage.removeItem('adlab_records_v1'); 1")
        b.goto(base + "/")
        check("records hidden for a first-time visitor",
              b.eval("document.getElementById('records').hidden"))

        # ---- returning visitors -------------------------------------------
        # docs/audience.md rests the six-week decision on "returning sessions
        # above 15%", and that was unmeasurable until now. These assertions
        # exist because the fix is a PRIVACY decision as much as a metric one:
        # the browser must transmit which bucket it falls into, and never the
        # date it computed that from.
        # ---- the daily hub ----------------------------------------------
        # Puzzmo's structure, and the Product agent's strategic pick (#56):
        # a page that remembers what you did today, so six games read as one
        # visit rather than six links.
        print("\nthe daily hub")
        b.goto(base + "/")
        b.eval("localStorage.removeItem('adlab_today_v1'); 1")
        b.goto(base + "/")

        check("a first visit shows no results and no count",
              b.eval("document.getElementById('today-summary').hidden && "
                     "[...document.querySelectorAll('.gc-today')].every(e=>e.hidden)"),
              "something was shown before anything was played")
        check("every card names its game",
              b.eval("document.querySelectorAll('.game-card[data-game]').length") == 6)

        # Record a day's play through the same event the games emit.
        b.eval("AdLab.track('game_end',{game:'2048',outcome:'lose',score:1024});"
               "AdLab.track('game_end',{game:'memory',outcome:'win',moves:22}); 1")
        b.goto(base + "/")
        check("what you played today is on the card",
              b.eval("document.querySelector('[data-game=\"2048\"] .gc-today').textContent")
              == "Scored 1024")
        check("memory reads in moves, not points",
              b.eval("document.querySelector('[data-game=\"memory\"] .gc-today').textContent")
              == "Done in 22 moves")
        check("a played card is marked as played",
              b.eval("document.querySelector('[data-game=\"2048\"]').getAttribute('data-played')")
              == "today")
        check("untouched games stay blank",
              b.eval("document.querySelector('[data-game=\"snake\"] .gc-today').hidden"))
        check("the count appears once there is something to count",
              b.eval("var s=document.getElementById('today-summary');"
                     "!s.hidden && s.textContent") == "2 games played today.")

        # The hub is about TODAY. A three-day-old win is worse than nothing,
        # because it removes the reason to come back.
        b.eval("var r=JSON.parse(localStorage.getItem('adlab_today_v1'));"
               "r.date='2020-01-01'; localStorage.setItem('adlab_today_v1',JSON.stringify(r)); 1")
        b.goto(base + "/")
        check("yesterday's results do not linger",
              b.eval("document.getElementById('today-summary').hidden && "
                     "[...document.querySelectorAll('.gc-today')].every(e=>e.hidden)"),
              "a stale day was shown as today")

        # ---- the daily archive ------------------------------------------
        # Every past puzzle is reproducible from its date alone, so the archive
        # costs nothing to store. The things that CAN go wrong are all about
        # honesty: handing out tomorrow's puzzle, or letting the archive inflate
        # a streak that is supposed to mean "you came back".
        print("\nthe daily archive")

        b.goto(base + "/daily/")
        today_num = b.eval("document.getElementById('dy-number').textContent")
        check("today has no forward link",
              b.eval("document.getElementById('dy-next').hidden"),
              "tomorrow's puzzle was reachable")
        check("today shows no archive band",
              b.eval("document.getElementById('dy-archive').hidden"))
        check("today links back one day",
              "?d=" in b.eval("document.getElementById('dy-prev').getAttribute('href')"))

        # A past day: a different puzzle, and it says which day it is.
        past = b.eval("document.getElementById('dy-prev').getAttribute('href')")
        b.goto(base + past.replace("/daily", "/daily/"))
        check("a past day is a different puzzle",
              b.eval("document.getElementById('dy-number').textContent") != today_num,
              "the archive served today's puzzle")
        check("it says which day you are on",
              not b.eval("document.getElementById('dy-archive').hidden"))
        check("and offers a way back to today",
              b.eval("!!document.querySelector('#dy-archive a[href=\"/daily\"]')"))
        check("a past day can go forward again",
              not b.eval("document.getElementById('dy-next').hidden"))

        # The dishonest cases. A future date must fall back to today rather
        # than handing out a puzzle nobody else has yet.
        b.goto(base + "/daily/?d=2099-01-01")
        check("a future date falls back to today",
              b.eval("document.getElementById('dy-number').textContent") == today_num,
              "a future puzzle was served")
        b.goto(base + "/daily/?d=not-a-date")
        check("a malformed date falls back to today",
              b.eval("document.getElementById('dy-number').textContent") == today_num)

        # And the streak must not be runnable by playing last week.
        b.goto(base + "/daily/")
        b.eval("localStorage.setItem('daily_streak', JSON.stringify({n:5,last:'2020-01-01'})); 1")
        b.goto(base + past.replace("/daily", "/daily/"))
        b.eval("AdLab.track('game_end',{game:'daily',outcome:'win',seconds:60}); 1")
        streak = json.loads(b.eval(
            "JSON.stringify(JSON.parse(localStorage.getItem('daily_streak')))"))
        check("an archive puzzle does not touch the streak",
              streak["n"] == 5 and streak["last"] == "2020-01-01", str(streak))

        # ---- near-miss feedback -----------------------------------------
        # Proposed by the Product agent (#56): tell a player how close they
        # came, so a loss carries information. The wording is asserted rather
        # than four games driven to a loss -- the message is the feature.
        print("\nnear-miss feedback")
        b.goto(base + "/2048/")

        def near(game, ev, best):
            return b.eval(
                "JSON.stringify(NearMiss.message(" + json.dumps({**ev, "game": game})
                + ", " + json.dumps(best) + "))")

        check("below your best, it says how far",
              json.loads(near("2048", {"score": 1900}, 2048)) == "148 points from your best of 2048.",
              near("2048", {"score": 1900}, 2048))
        check("beating it says so",
              "New best" in json.loads(near("2048", {"score": 4096}, 2048)),
              near("2048", {"score": 4096}, 2048))
        check("matching it is called out",
              "matched" in json.loads(near("2048", {"score": 2048}, 2048)),
              near("2048", {"score": 2048}, 2048))
        check("one point reads as singular",
              json.loads(near("2048", {"score": 2047}, 2048)) == "1 point from your best of 2048.",
              near("2048", {"score": 2047}, 2048))

        # Memory is a race: FEWER moves is better. Treating it like a score is
        # how a personal best ends up recording someone's worst round.
        check("fewer moves beats more in memory",
              "New best" in json.loads(near("memory", {"moves": 20}, 30)),
              near("memory", {"moves": 20}, 30))
        check("more moves is the near miss in memory",
              json.loads(near("memory", {"moves": 34}, 30)) == "4 moves from your best of 30.",
              near("memory", {"moves": 34}, 30))

        # A first game has nothing to be close to, and inventing encouragement
        # for it would make the real message worth less.
        check("a first game says nothing", json.loads(near("2048", {"score": 500}, None)) == "",
              near("2048", {"score": 500}, None))
        # "How close" in an adversarial game needs position analysis we do not
        # do, and saying something vague is worse than saying nothing.
        check("adversarial games say nothing", json.loads(near("xo", {"score": 1}, 1)) == "",
              near("xo", {"score": 1}, 1))

        check("the line exists and starts hidden",
              b.eval("var e=document.getElementById('near-miss'); !!e && e.hidden"))
        # It must sit BELOW the actions: a line that pushes the replay button
        # down delays an action the player has already decided to take.
        check("it renders below the replay button",
              b.eval("(function(){var n=document.getElementById('near-miss'),"
                     "a=document.querySelector('.actions');"
                     "return !!(n&&a)&&(a.compareDocumentPosition(n)&4)>0})()"))

        # ---- distribution assets ----------------------------------------
        # Checked against the BUILD RECIPES, not against files on disk.
        #
        # dist/ is generated and ignored, so an on-disk check passes locally and
        # fails in CI -- which is exactly what the first version of this did.
        # The invariant that actually matters is that a listing never names an
        # asset the generators will not produce, and that survives a clean
        # checkout.
        print("\ndistribution assets")
        import re as _re
        listings = pathlib.Path("dist/itch/LISTINGS.md").read_text()
        pkg = pathlib.Path("tools/build/itch-package.py").read_text()
        cov = pathlib.Path("tools/build/itch-cover/render.py").read_text()

        listed_zips = set(_re.findall(r"`([a-z0-9-]+)\.zip`", listings))
        listed_covers = set(_re.findall(r"covers/([a-z0-9-]+)\.png", listings))
        built_zips = set(_re.findall(r'^\s*"([a-z0-9-]+)":\s*\(', pkg, _re.M))
        built_covers = set(_re.findall(r'^\s*\("([a-z0-9-]+)",', cov, _re.M))

        check("six games are listed", len(listed_zips) == 6, str(sorted(listed_zips)))
        check("every listed zip is one the packager builds",
              listed_zips == built_zips,
              f"listed {sorted(listed_zips)} vs built {sorted(built_zips)}")
        check("every listed cover is one the renderer draws",
              listed_covers == built_covers,
              f"listed {sorted(listed_covers)} vs drawn {sorted(built_covers)}")
        check("each game has both a zip and a cover",
              listed_zips == listed_covers,
              f"zips {sorted(listed_zips)} vs covers {sorted(listed_covers)}")

        print("\nreturning visitors")
        b.goto(base + "/")

        def visit(days_ago=None):
            """Set the stored last-visit date, then ask for the status."""
            if days_ago is None:
                setup = "localStorage.removeItem('adlab_last_visit_v1');"
            else:
                setup = (f"var d=new Date();d.setUTCDate(d.getUTCDate()-{days_ago});"
                         "localStorage.setItem('adlab_last_visit_v1',"
                         "d.toISOString().slice(0,10));")
            return json.loads(b.eval(setup + "JSON.stringify(AdLab.visitStatus())"))

        check("a first visit is new, not returning",
              visit(None) == {"returning": False, "bucket": "new"}, str(visit(None)))
        check("same day is d0", visit(0)["bucket"] == "d0", str(visit(0)))
        check("three days ago is d1_7", visit(3)["bucket"] == "d1_7", str(visit(3)))
        check("seven days ago is still d1_7", visit(7)["bucket"] == "d1_7", str(visit(7)))
        check("eight days ago falls out of the week", visit(8)["bucket"] == "d8_30",
              str(visit(8)))
        check("forty days ago is d30_plus", visit(40)["bucket"] == "d30_plus",
              str(visit(40)))

        # The privacy property, asserted rather than assumed: every returning
        # browser must send one of five constants, and never the stored date.
        # A date is low entropy on its own and a contribution to a fingerprint
        # in combination, and we do not need it to answer the question.
        payload = b.eval(
            "var d=new Date();d.setUTCDate(d.getUTCDate()-3);"
            "localStorage.setItem('adlab_last_visit_v1',d.toISOString().slice(0,10));"
            "JSON.stringify(AdLab.visitStatus())")
        check("the transmitted value carries no date",
              not re.search(r"\d{4}-\d{2}-\d{2}", payload), payload)
        check("the transmitted value is one of five constants",
              json.loads(payload)["bucket"] in
              {"new", "d0", "d1_7", "d8_30", "d30_plus", "unknown"}, payload)
    finally:
        b.close()


if __name__ == "__main__":
    sys.exit(main())
