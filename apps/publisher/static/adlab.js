/* ---------------------------------------------------------------------------
 * adlab.js — session identity + product analytics transport.
 *
 * Deliberately NOT an advertising identifier. See docs/privacy-baseline.md:
 *   scope       one browsing session in one tab
 *   storage     sessionStorage only
 *   lifetime    dies when the tab closes; never restored
 *   used for    joining product events to ad events within one visit
 *   NOT used for  targeting, frequency capping, or cross-visit recognition
 *
 * Events are BATCHED. Sending ~13 product events per session individually
 * would make product analytics outnumber ad requests ~9:1, dominating both
 * the cost model and the Cost Fuse signal. See docs/cost-model.md.
 * ------------------------------------------------------------------------- */
window.AdLab = (function () {
  'use strict';

  var ENDPOINT   = '/collect';
  var MAX_EVENTS = 50;      // matches the server-side batch cap
  var MAX_BYTES  = 32 * 1024;

  function newId() {
    if (window.crypto && crypto.randomUUID) return crypto.randomUUID();
    return 'sid-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
  }

  var sessionId;
  try {
    sessionId = sessionStorage.getItem('adlab_sid');
    if (!sessionId) { sessionId = newId(); sessionStorage.setItem('adlab_sid', sessionId); }
  } catch (e) {
    sessionId = newId();            // private mode, storage blocked: in-memory only
  }

  var queue = [];

  function flush(useBeacon) {
    if (!queue.length) return;
    var payload = JSON.stringify({ session_id: sessionId, events: queue });
    queue = [];
    try {
      if (useBeacon && navigator.sendBeacon) {
        navigator.sendBeacon(ENDPOINT, new Blob([payload], { type: 'application/json' }));
      } else {
        fetch(ENDPOINT, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: payload,
          keepalive: true
        }).catch(function () { /* analytics must never break the game */ });
      }
    } catch (e) { /* same */ }
  }

  function track(name, props) {
    var ev = props || {};
    ev.event = name;
    ev.ts = Date.now();
    // Every event carries the arm, so revenue and replay rate can both be
    // attributed to it. An unlogged assignment is an unmeasurable experiment.
    try { ev.endgame_arm = endgameArm(); } catch (e) {}
    if (name === 'game_end') { recordResult(ev); revealEndgame(); }
    queue.push(ev);
    if (queue.length >= MAX_EVENTS || JSON.stringify(queue).length >= MAX_BYTES) flush(false);
  }

  // ---- End-of-game slot -------------------------------------------------
  //
  // Revealed only after game_end, and it lives BELOW the actions, so filling it
  // never moves the replay button. A placement that delays an action the player
  // has already decided to take is the one thing docs/ad-placement.md rules
  // out unconditionally -- "play again" is the only engagement signal we have.
  //
  // Bucketed per session and logged, because a placement ships permanently only
  // if replay rate is no worse, and that comparison needs an arm recorded on
  // every event.
  function endgameArm() {
    try {
      var v = sessionStorage.getItem('adlab_endgame_arm');
      if (v) return v;
      // Hash the session id rather than rolling a die, so the arm is stable for
      // the whole session and reproducible from the logs.
      var h = 2166136261 >>> 0, sid = String(sessionId);
      for (var i = 0; i < sid.length; i++) {
        h ^= sid.charCodeAt(i);
        h = Math.imul(h, 16777619) >>> 0;
      }
      v = (h % 2 === 0) ? 'on' : 'off';
      sessionStorage.setItem('adlab_endgame_arm', v);
      return v;
    } catch (e) {
      return 'off';   // no storage: no unstable arm, and no new slot
    }
  }

  function revealEndgame() {
    try {
      var el = document.getElementById('endgame');
      if (!el || !el.hidden) return;
      if (endgameArm() !== 'on') return;
      el.hidden = false;
      track('ad_slot_revealed', { placement_id: 'game_end' });
    } catch (e) {}
  }

  // ---- Records ----------------------------------------------------------
  // Every game already reports game_end through track(), so hooking it here
  // gives all four games a personal-best table with one change instead of four.
  //
  // Deliberately LOCAL only. A global leaderboard needs a server, an identity,
  // and an answer to cheating -- scores come from the client, so a global table
  // is a table of whoever edited their score first. Local records are honest,
  // cost nothing, add no identifier, and are the part players actually return
  // for. See docs/publisher-product.md.
  var RECORDS_KEY = 'adlab_records_v1';

  function readRecords() {
    try {
      return JSON.parse(localStorage.getItem(RECORDS_KEY)) || {};
    } catch (e) {
      return {};
    }
  }

  function recordResult(ev) {
    if (!ev.game) return;
    try {
      var all = readRecords();
      var r = all[ev.game] || { played: 0, won: 0, best: null };
      r.played++;
      if (ev.outcome === 'win') r.won++;

      // Not every game agrees on which direction is better. Memory and the
      // daily are races -- fewer moves and fewer seconds win -- and treating
      // them like a score recorded the player's WORST round as their best.
      // The table lives in game-room.js, which ships everywhere including the
      // itch builds; this reads it when present and falls back to higher-is-
      // better, which is right for the two games that have a score.
      var rule = (window.NearMiss && window.NearMiss.scoring[ev.game]) || null;
      var field = rule ? rule.field : 'score';
      var lower = rule ? rule.better === 'lower' : false;
      var value = Number(ev[field]);
      if (isFinite(value)) {
        var first = r.best === null || !isFinite(Number(r.best));
        if (first || (lower ? value < r.best : value > r.best)) r.best = value;
      }
      all[ev.game] = r;
      localStorage.setItem(RECORDS_KEY, JSON.stringify(all));
    } catch (e) { /* storage disabled -- records are a nicety, never a blocker */ }
  }

  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'hidden') flush(true);
  });
  window.addEventListener('pagehide', function () { flush(true); });

  function deviceType() {
    return matchMedia('(pointer: coarse)').matches ? 'mobile' : 'desktop';
  }

  // ---- Have they been here before? --------------------------------------
  //
  // docs/audience.md rests the entire six-week decision on "returning sessions
  // above 15%", and until now that was unmeasurable: session identity lives in
  // sessionStorage, so the same person on Tuesday and on Thursday were two
  // strangers.
  //
  // The fix is NOT a persistent identifier. It is a boolean.
  //
  // We keep one date locally -- 'YYYY-MM-DD', low entropy, first party, never
  // transmitted -- and send only which BUCKET the gap falls into. Every
  // returning browser sends the identical string, so the transmitted value
  // cannot single anyone out, cannot be joined to anything, and says nothing
  // about a person beyond the fact that this browser has been here.
  //
  // Compute on the client, transmit the answer rather than the data. It is the
  // same principle as on-device attribution, and it is the difference between
  // answering the question and building a profile to answer it with.
  var VISIT_KEY = 'adlab_last_visit_v1';

  function today() {
    return new Date().toISOString().slice(0, 10);   // YYYY-MM-DD, UTC
  }

  function daysBetween(a, b) {
    var ms = Date.parse(b + 'T00:00:00Z') - Date.parse(a + 'T00:00:00Z');
    return isFinite(ms) ? Math.round(ms / 86400000) : null;
  }

  // Returns { returning: bool|null, bucket: string }. A null means storage is
  // unavailable -- a private window, or storage blocked -- which is NOT the
  // same as a first visit. Reporting those as new would understate the
  // returning rate by exactly the number of privacy-conscious players, who are
  // not a random sample.
  function visitStatus() {
    var now = today();
    var last;
    try {
      last = localStorage.getItem(VISIT_KEY);
      localStorage.setItem(VISIT_KEY, now);
    } catch (e) {
      return { returning: null, bucket: 'unknown' };
    }
    if (!last) return { returning: false, bucket: 'new' };

    var d = daysBetween(last, now);
    if (d === null || d < 0) return { returning: null, bucket: 'unknown' };
    if (d === 0) return { returning: true, bucket: 'd0' };
    if (d <= 7) return { returning: true, bucket: 'd1_7' };
    if (d <= 30) return { returning: true, bucket: 'd8_30' };
    return { returning: true, bucket: 'd30_plus' };
  }

  // session_start once per tab-session; game_view on every page load.
  function startPayload(referrerClass) {
    var v = visitStatus();
    var p = {
      device_type: deviceType(),
      referrer_class: referrerClass,
      returning_bucket: v.bucket
    };
    // Omitted entirely when unknown, so the field is absent rather than false.
    // A cold-path reader can then tell "did not come back" from "could not
    // tell", which are opposite findings.
    if (v.returning !== null) p.returning = v.returning;
    return p;
  }

  try {
    if (!sessionStorage.getItem('adlab_started')) {
      sessionStorage.setItem('adlab_started', '1');
      track('session_start', startPayload(document.referrer ? 'external' : 'direct'));
    }
  } catch (e) {
    track('session_start', startPayload('unknown'));
  }

  return {
    sessionId: sessionId,
    track: track,
    flush: flush,
    deviceType: deviceType,
    records: readRecords,
    visitStatus: visitStatus
  };
})();
