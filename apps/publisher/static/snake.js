/* ---------------------------------------------------------------------------
 * Snake.
 *
 * Canvas rather than a DOM grid: this redraws every tick, and 400 DOM nodes
 * mutating 10 times a second is the wrong tool.
 *
 * Controls are arrows, WASD, and swipe -- mobile is the majority of traffic, so
 * touch is not an afterthought.
 * ------------------------------------------------------------------------- */
(function () {
  'use strict';

  var canvas = document.getElementById('snake');
  if (!canvas) return;

  var GR = window.GameRoom || {};
  var beep = GR.beep || function () {};
  var tune = GR.tune || function () {};
  var say = GR.say || function () {};
  var getVar = GR.getVar || function () { return '#333'; };

  var COLS = 20, ROWS = 20;
  var SPEEDS = [150, 105, 70];            // easy, medium, hard: ms per tick

  var ctx = canvas.getContext('2d');
  var scoreEl = document.getElementById('sn-score');
  var bestEl = document.getElementById('sn-best');
  var lenEl = document.getElementById('sn-len');
  var statusEl = document.getElementById('sn-status');
  var statusText = document.getElementById('sn-status-text');
  var againBtn = document.getElementById('sn-again');
  var levelEl = document.getElementById('sn-level');
  var barEl = document.getElementById('sn-bar-fill');
  var barWrap = document.getElementById('sn-bar');
  var levelPop = document.getElementById('sn-levelpop');
  var boardEl = document.querySelector('.snake-wrap');

  var snake, dir, nextDir, food, score, best, alive, started, timer;

  // `band` is the difficulty CHOICE (Slow/Normal/Fast). `level` is in-run
  // progression, which is a different thing and used to share this name.
  var band = 1;
  var level, apples, applesIntoLevel, bestLevel;

  // Levels exist to make depth matter more than grinding. The apple is worth
  // 10 x level, so one run to level 7 beats twenty runs to level 3 -- which is
  // what creates the pull to go further rather than to play more often.
  //
  // Deliberately NOT built on loss aversion: no timers, no streaks to break, no
  // variable-ratio rewards. The site is for all ages, and the difference
  // between satisfying and compulsive is whether the pressure comes from
  // wanting the next level or from fearing the loss of something already held.
  function applesForLevel(n) { return 2 + n; }   // L1->L2 = 3, then 4, 5, ...
  function scoreForApple(n) { return 10 * n; }   // depth beats grinding
  function tickForLevel(base, n) {
    return Math.max(base * 0.45, base - (n - 1) * (base * 0.06));
  }

  // Exposed for tests. PURE FUNCTIONS ONLY -- no state, no DOM, no way to
  // affect a real game. Driving Snake open-loop from a test harness proved
  // unreliable (no positional feedback, so timing drift walks it into a wall),
  // and the honest fix is to make the arithmetic checkable rather than to keep
  // trying to play well.
  window.SnakeMath = {
    applesForLevel: applesForLevel,
    scoreForApple: scoreForApple,
    tickForLevel: tickForLevel
  };

  // Speed accelerates with level, within the chosen band, and stops
  // accelerating before it becomes unreadable.
  function tickMS() { return tickForLevel(SPEEDS[band], level); }

  // A local best score. NOT an identifier: it never leaves the device, is not
  // sent anywhere, and identifies nobody. See docs/privacy-baseline.md.
  // Best level survives every failed run. Meta-progress that a death cannot
  // take away is what makes a hard run feel spent rather than wasted.
  try { bestLevel = parseInt(localStorage.getItem('snake_best_level') || '1', 10) || 1; }
  catch (e) { bestLevel = 1; }
  try { best = parseInt(localStorage.getItem('snake_best') || '0', 10) || 0; }
  catch (e) { best = 0; }

  function fit() {
    var dpr = window.devicePixelRatio || 1;
    var size = canvas.clientWidth;
    canvas.width = size * dpr;
    canvas.height = size * dpr;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    draw();
  }

  function cell() { return canvas.clientWidth / COLS; }

  function placeFood() {
    var open = [];
    for (var y = 0; y < ROWS; y++) {
      for (var x = 0; x < COLS; x++) {
        var taken = snake.some(function (s) { return s.x === x && s.y === y; });
        if (!taken) open.push({ x: x, y: y });
      }
    }
    food = open.length ? open[(Math.random() * open.length) | 0] : null;
  }

  function reset() {
    snake = [{ x: 9, y: 10 }, { x: 8, y: 10 }, { x: 7, y: 10 }];
    dir = { x: 1, y: 0 };
    nextDir = dir;
    score = 0;
    level = 1;
    apples = 0;
    applesIntoLevel = 0;
    alive = true;
    started = false;
    placeFood();
    render();
    statusEl.classList.remove('over');
    statusText.textContent = 'Press an arrow key, or swipe';
    draw();
  }

  function render() {
    scoreEl.textContent = score;
    bestEl.textContent = best;
    lenEl.textContent = snake.length;
    renderLevel();
  }

  function roundRect(x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.arcTo(x + w, y, x + w, y + h, r);
    ctx.arcTo(x + w, y + h, x, y + h, r);
    ctx.arcTo(x, y + h, x, y, r);
    ctx.arcTo(x, y, x + w, y, r);
    ctx.closePath();
    ctx.fill();
  }

  function draw() {
    if (!snake) return;
    var c = cell(), pad = c * 0.10, r = c * 0.28;

    ctx.clearRect(0, 0, canvas.clientWidth, canvas.clientWidth);

    if (food) drawApple(food.x * c, food.y * c, c);

    for (var i = snake.length - 1; i >= 0; i--) {
      // The head is the accent colour so it is obvious which end you steer.
      ctx.fillStyle = i === 0 ? (getVar('--o') || '#0FAE9A') : (getVar('--frame') || '#4A4FA8');
      ctx.globalAlpha = i === 0 ? 1 : Math.max(0.45, 1 - i / (snake.length + 6));
      roundRect(snake[i].x * c + pad, snake[i].y * c + pad, c - pad * 2, c - pad * 2, r);
    }
    ctx.globalAlpha = 1;
  }

  // An apple, not a square. The food is the thing the player looks at most in
  // a game of Snake, and a rounded rectangle is the one object on screen that
  // could have character and did not.
  //
  // Drawn rather than an image: at this size a sprite would need three
  // resolutions and a load-order problem, and the whole shape is two circles,
  // a leaf and a highlight.
  function drawApple(x, y, c) {
    var cx = x + c / 2, cy = y + c * 0.56, r = c * 0.32;

    // Body: two overlapping lobes give the dent at the top that reads as an
    // apple rather than a ball.
    ctx.fillStyle = '#E8434F';
    ctx.beginPath();
    ctx.arc(cx - r * 0.28, cy, r, 0, Math.PI * 2);
    ctx.arc(cx + r * 0.28, cy, r, 0, Math.PI * 2);
    ctx.fill();

    // Stalk.
    ctx.strokeStyle = '#7A4A28';
    ctx.lineWidth = Math.max(1, c * 0.06);
    ctx.lineCap = 'round';
    ctx.beginPath();
    ctx.moveTo(cx, cy - r * 0.92);
    ctx.quadraticCurveTo(cx + c * 0.04, cy - r * 1.5, cx + c * 0.10, cy - r * 1.6);
    ctx.stroke();

    // Leaf.
    ctx.fillStyle = '#2BC4AE';
    ctx.beginPath();
    ctx.ellipse(cx + c * 0.13, cy - r * 1.35, c * 0.11, c * 0.06, -0.5, 0, Math.PI * 2);
    ctx.fill();

    // Highlight, top-left, matching the light source used everywhere else.
    ctx.fillStyle = 'rgba(255,255,255,0.55)';
    ctx.beginPath();
    ctx.ellipse(cx - r * 0.45, cy - r * 0.45, r * 0.30, r * 0.20, -0.6, 0, Math.PI * 2);
    ctx.fill();
  }

  function step() {
    if (!alive) return;
    dir = nextDir;

    var head = { x: snake[0].x + dir.x, y: snake[0].y + dir.y };

    // Walls and self are both fatal. No wrap-around: it makes the game
    // meaningfully easier and removes most of the tension.
    var hitWall = head.x < 0 || head.y < 0 || head.x >= COLS || head.y >= ROWS;
    var hitSelf = snake.some(function (s) { return s.x === head.x && s.y === head.y; });
    if (hitWall || hitSelf) return die();

    snake.unshift(head);

    if (food && head.x === food.x && head.y === food.y) {
      score += scoreForApple(level);
      apples++;
      applesIntoLevel++;
      beep(660, 0.08, 'square', 0.12);
      if (applesIntoLevel >= applesForLevel(level)) levelUp();
      placeFood();
      if (!food) return win();
    } else {
      snake.pop();
    }

    render();
    draw();
  }

  // The level-up moment. Under 400ms on purpose: long enough to register as a
  // reward, short enough not to interrupt the run that earned it.
  // Milestone levels get a bigger celebration than ordinary ones. If level 10
  // feels the same as level 2, the ladder has no shape and reaching the top of
  // it means nothing.
  function isMilestone(n) { return n % 5 === 0; }

  // Haptics. Short and rare on purpose -- a phone that buzzes constantly gets
  // its permission revoked by the user, not by the browser.
  function buzz(pattern) {
    try { if (navigator.vibrate) navigator.vibrate(pattern); } catch (e) {}
  }

  function levelUp() {
    level++;
    applesIntoLevel = 0;
    if (level > bestLevel) {
      bestLevel = level;
      try { localStorage.setItem('snake_best_level', String(bestLevel)); } catch (e) {}
    }
    clearInterval(timer);
    timer = setInterval(step, tickMS());

    if (boardEl) {
      boardEl.classList.remove('sn-levelup');
      void boardEl.offsetWidth;              // restart the animation
      boardEl.classList.add('sn-levelup');
    }
    var big = isMilestone(level);

    if (levelPop) {
      levelPop.textContent = big ? 'LEVEL ' + level + '!' : 'Level ' + level;
      levelPop.classList.remove('show', 'big');
      void levelPop.offsetWidth;
      levelPop.classList.add('show');
      if (big) levelPop.classList.add('big');
    }

    // Sound escalates with the moment. A rising figure reads as progress in a
    // way a single beep does not; a milestone gets a full arpeggio so that
    // reaching one is audibly different from ordinary progress.
    var notes = big ? [660, 830, 990, 1320] : [720, 960];
    notes.forEach(function (hz, i) {
      setTimeout(function () { beep(hz, big ? 0.11 : 0.1, 'square', 0.14); }, i * 85);
    });

    // Three channels converging on the same instant is what makes a reward
    // land: sight, sound and touch.
    buzz(big ? [40, 60, 40, 60, 90] : 35);
    if (big) (GR.confetti || function () {})(getVar('--gold'));
  }

  function renderLevel() {
    if (levelEl) levelEl.textContent = level;
    if (barEl) {
      var need = applesForLevel(level);
      var pct = Math.min(100, (applesIntoLevel / need) * 100);
      barEl.style.width = pct + '%';
      // Anticipation. A reward lands harder when you saw it coming, so the bar
      // starts pulsing while you are still one or two apples away -- the
      // tension before the level-up is doing as much work as the level-up.
      barEl.classList.toggle('near', pct >= 70 && pct < 100);
      if (barWrap) {
        barWrap.setAttribute('aria-valuenow', String(applesIntoLevel));
        barWrap.setAttribute('aria-valuemax', String(need));
        barWrap.setAttribute('aria-label',
          'Level ' + level + ': ' + applesIntoLevel + ' of ' + need + ' to next level');
      }
    }
  }

  function die() {
    alive = false;
    clearInterval(timer);
    statusEl.classList.add('over');

    var isBest = score > best;
    if (isBest) {
      best = score;
      try { localStorage.setItem('snake_best', String(best)); } catch (e) {}
    }
    render();

    // The near-miss. "You were 2 apples from Level 6" pulls far harder than
    // "you lost", because it makes the next attempt feel already half-won.
    var need = applesForLevel(level) - applesIntoLevel;
    var nearMiss = (level >= 2 || applesIntoLevel > 0)
      ? ' — ' + need + (need === 1 ? ' apple' : ' apples') + ' from Level ' + (level + 1)
      : '';
    statusText.textContent = isBest && score > 0
      ? 'New best: ' + score + '!' + nearMiss
      : 'Level ' + level + ' — ' + score + ' points' + nearMiss;
    say(statusText.textContent);
    tune([330, 262], 0.16);

    window.AdLab && AdLab.track('game_end', {
      game: 'snake', outcome: 'lose', speed_band: band, level: level,
      score: score, length: snake.length, best: best, apples: apples
    });
  }

  function win() {
    alive = false;
    clearInterval(timer);
    statusEl.classList.add('over');
    statusText.textContent = 'You filled the board. Genuinely impressive.';
    say(statusText.textContent);
    tune([523, 659, 784, 1047], 0.1);
    (GR.confetti || function () {})(getVar('--gold'));
    window.AdLab && AdLab.track('game_end', { game: 'snake', outcome: 'win', speed_band: band, level: level, score: score });
  }

  function start() {
    if (started || !alive) return;
    started = true;
    statusText.textContent = 'Go';
    clearInterval(timer);
    timer = setInterval(step, tickMS());
    window.AdLab && AdLab.track('game_start', { game: 'snake', mode: 'solo', speed_band: band });
  }

  function turn(x, y) {
    // Cannot reverse into yourself -- the classic Snake rule, and without it
    // the game is trivially unlosable.
    if (dir.x === -x && dir.y === -y) return;
    nextDir = { x: x, y: y };
    start();
  }

  var KEYS = {
    ArrowUp: [0, -1], ArrowDown: [0, 1], ArrowLeft: [-1, 0], ArrowRight: [1, 0],
    w: [0, -1], s: [0, 1], a: [-1, 0], d: [1, 0],
    W: [0, -1], S: [0, 1], A: [-1, 0], D: [1, 0]
  };

  window.addEventListener('keydown', function (e) {
    var k = KEYS[e.key];
    if (!k) return;
    e.preventDefault();           // stop arrows scrolling the page mid-game
    turn(k[0], k[1]);
  });

  // --- touch: swipe anywhere on the board ---
  var t0 = null;
  canvas.addEventListener('touchstart', function (e) {
    t0 = { x: e.touches[0].clientX, y: e.touches[0].clientY };
  }, { passive: true });

  canvas.addEventListener('touchend', function (e) {
    if (!t0) return;
    var dx = e.changedTouches[0].clientX - t0.x;
    var dy = e.changedTouches[0].clientY - t0.y;
    t0 = null;
    if (Math.abs(dx) < 20 && Math.abs(dy) < 20) return;      // a tap, not a swipe
    if (Math.abs(dx) > Math.abs(dy)) turn(dx > 0 ? 1 : -1, 0);
    else turn(0, dy > 0 ? 1 : -1);
  }, { passive: true });

  againBtn.addEventListener('click', function () {
    window.AdLab && AdLab.track('game_replay', { game: 'snake' });
    clearInterval(timer);
    reset();
  });

  [].forEach.call(document.querySelectorAll('#sn-levels .chip'), function (b) {
    b.addEventListener('click', function () {
      band = +b.dataset.level;
      [].forEach.call(document.querySelectorAll('#sn-levels .chip'), function (o) {
        o.setAttribute('aria-pressed', String(o === b));
      });
      clearInterval(timer);
      reset();
    });
  });

  // Pause when the tab is hidden. Dying because you switched tabs is infuriating.
  document.addEventListener('visibilitychange', function () {
    if (document.hidden) clearInterval(timer);
    else if (started && alive) timer = setInterval(step, tickMS());
  });

  window.addEventListener('resize', fit);
  reset();
  fit();
})();
