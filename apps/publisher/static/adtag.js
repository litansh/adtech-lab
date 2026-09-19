/* ---------------------------------------------------------------------------
 * adtag.js — the ad tag.
 *
 * Knows nothing about the game. The game knows nothing about it. They share
 * only a session_id and the AdLab transport. That boundary is what makes
 * "does advertising hurt the product?" an answerable question.
 *
 * Hard rule: if anything here fails, the slot collapses and the game is
 * unaffected. The ad server is never allowed to break the Publisher.
 * ------------------------------------------------------------------------- */
(function () {
  'use strict';

  var slot = document.getElementById('ad-slot');
  if (!slot || !window.AdLab) return;

  var placementId = slot.dataset.placement || 'game_sidebar';
  var game        = slot.dataset.game || 'unknown';

  function collapse() { slot.dataset.filled = '0'; }

  // Belt and braces: if the slot is not in the layout tree at all, the
  // observer can never fire and the ad would never be requested. Request
  // immediately rather than waiting for an event that cannot happen.
  function observable(el) {
    return el.offsetParent !== null || el.getClientRects().length > 0 ||
           getComputedStyle(el).display !== 'none';
  }

  function fire(url) {
    // Tracking beacons are GETs so they survive page unload.
    try { (new Image()).src = url; } catch (e) {}
  }

  function render(ad, requestId) {
    // Creative runs in a sandboxed iframe: it can paint and be clicked, but it
    // cannot reach the Publisher page, its storage, or its session.
    var f = document.createElement('iframe');
    f.setAttribute('sandbox', 'allow-scripts');
    f.setAttribute('loading', 'lazy');
    f.setAttribute('title', 'Advertisement');
    f.setAttribute('scrolling', 'no');
    f.srcdoc = ad.html;
    slot.appendChild(f);
    slot.dataset.filled = '1';

    fire('/event/impression?token=' + encodeURIComponent(ad.tracking_token));
    AdLab.track('ad_impression', { request_id: requestId, placement_id: placementId, game: game });

    slot.addEventListener('click', function () {
      fire('/event/click?token=' + encodeURIComponent(ad.tracking_token));
      AdLab.track('ad_click', { request_id: requestId, placement_id: placementId, game: game });
    });

    watchViewability(requestId);
  }

  var body = {
    placement_id: placementId,
    game:         game,
    session_id:   AdLab.sessionId,
    device_type:  AdLab.deviceType(),
    w: 300, h: 250
  };

  // ---- Viewability -------------------------------------------------------
  //
  // The MRC standard for a display ad: at least 50% of its pixels in the
  // viewport for at least ONE CONTINUOUS second. Every word is load-bearing.
  //
  //   50%        an ad half off the bottom of the screen was not seen
  //   continuous a second of attention, not a second of scrolling past
  //   viewport   geometry, not "the element exists"
  //
  // Three facts get reported, and the distinction between them is the point:
  //
  //   measurable  could we observe it at all?
  //   viewable    did it meet the standard?
  //   viewed_ms   how long it actually stayed
  //
  // An impression that is not MEASURABLE is not the same as one that failed to
  // be viewable, and reporting them together is how viewability rates become
  // meaningless. An ad in a browser without IntersectionObserver, or in a
  // cross-origin frame we cannot observe, is unmeasured -- not unviewed.
  var VIEW_FRACTION = 0.5;
  var VIEW_MS = 1000;

  function watchViewability(requestId) {
    var base = { request_id: requestId, placement_id: placementId, game: game };

    if (!('IntersectionObserver' in window)) {
      AdLab.track('ad_measurable', assign(base, { measurable: false, reason: 'no_observer' }));
      return;
    }
    AdLab.track('ad_measurable', assign(base, { measurable: true }));

    var timer = null, reported = false, inView = false;
    var enteredAt = 0, totalMs = 0;

    function stop() {
      if (timer) { clearTimeout(timer); timer = null; }
      if (inView) { totalMs += Date.now() - enteredAt; inView = false; }
    }

    function report() {
      if (reported) return;
      reported = true;
      AdLab.track('ad_viewable', assign(base, {
        viewable: true, threshold: VIEW_FRACTION, min_ms: VIEW_MS
      }));
    }

    var io = new IntersectionObserver(function (entries) {
      var e = entries[entries.length - 1];
      // A hidden tab is not viewable however the geometry looks. The standard
      // is about a person seeing it, and nobody is looking at a background tab.
      var visible = e.intersectionRatio >= VIEW_FRACTION &&
                    document.visibilityState === 'visible';
      if (visible && !inView) {
        inView = true;
        enteredAt = Date.now();
        // The timer is the "continuous" part. Scrolling away clears it, so a
        // second of scroll-past never counts as a second of attention.
        if (!reported) timer = setTimeout(report, VIEW_MS);
      } else if (!visible && inView) {
        stop();
      }
    }, { threshold: [0, VIEW_FRACTION, 1] });

    io.observe(slot);

    document.addEventListener('visibilitychange', function () {
      if (document.visibilityState !== 'visible') stop();
    });

    // Report the dwell time once, at the end. Sent even when the ad never
    // became viewable, because "in view for 400ms" and "never in view" are
    // different failures and only one of them is a placement problem.
    function finish() {
      stop();
      io.disconnect();
      AdLab.track('ad_view_time', assign(base, {
        viewed_ms: Math.round(totalMs), reached_standard: reported
      }));
    }
    window.addEventListener('pagehide', finish, { once: true });
    document.addEventListener('visibilitychange', function () {
      if (document.visibilityState === 'hidden') finish();
    }, { once: true });
  }

  function assign(a, b) {
    var out = {};
    for (var k in a) if (Object.prototype.hasOwnProperty.call(a, k)) out[k] = a[k];
    for (var j in b) if (Object.prototype.hasOwnProperty.call(b, j)) out[j] = b[j];
    return out;
  }

  // ---- Lazy request ------------------------------------------------------
  //
  // Do not request an ad until the slot is near the viewport.
  //
  // Measured on this site: the slot is 0% visible on load on every game, at
  // both a laptop and a phone viewport -- it needs 166-289px of scrolling to
  // reach the MRC threshold. That is the direct consequence of a deliberate
  // product decision (the board goes above the fold, so the ad goes below it),
  // and we are not reversing that decision. Putting the ad above the board is
  // the classic make-for-advertising move and CLAUDE.md forbids it.
  //
  // So instead: stop requesting ads nobody will see. Three things improve at
  // once, which is unusual enough to be worth stating:
  //
  //   COST      a request that is never seen still fans out to buyers, and
  //             buyer calls are 37.8% of our infrastructure cost per thousand
  //             requests -- more than Lambda, CloudFront and DynamoDB. An
  //             unviewed impression is close to pure cost. See docs/finops.md.
  //   QUALITY   viewability rate is impressions-viewable over impressions. Not
  //             counting impressions nobody could see raises it honestly,
  //             rather than by redefining the numerator.
  //   BUYER     a buyer paying on vCPM gets what they paid for, and one paying
  //             on CPM stops paying for inventory that was never seen.
  //
  // The player is unaffected, which is what makes this the right lever rather
  // than a trade-off.
  var REQUEST_MARGIN = '300px';

  function requestAd() {
    AdLab.track('ad_request', { placement_id: placementId, game: game });
    doFetch();
  }

  if ('IntersectionObserver' in window && observable(slot)) {
    var pre = new IntersectionObserver(function (entries) {
      if (entries.some(function (e) { return e.isIntersecting; })) {
        pre.disconnect();
        requestAd();
      }
    }, { rootMargin: REQUEST_MARGIN });
    pre.observe(slot);
  } else {
    // No observer means no way to know when the slot approaches the viewport.
    // Requesting immediately is the safe failure: it costs a request rather
    // than silently never monetising a browser we cannot measure.
    requestAd();
  }

  function doFetch() {
  fetch('/ad/request', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body)
  })
    .then(function (r) { return r.ok ? r.json() : Promise.reject(r.status); })
    .then(function (res) {
      if (res && res.ad) render(res.ad, res.request_id);
      else collapse();          // explicit no_ad: unfilled inventory, not an error
    })
    .catch(function () {
      collapse();               // no ad server yet, or it failed. Game unaffected.
    });
  }
})();
