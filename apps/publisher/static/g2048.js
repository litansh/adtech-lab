/* ---------------------------------------------------------------------------
 * 2048.
 *
 * DOM tiles rather than canvas: the whole feel of this game is tiles SLIDING,
 * and CSS transforms give that for free with GPU compositing. On canvas we
 * would be hand-animating every frame to get something worse.
 *
 * Three deliberate concessions to "all ages":
 *   - one level of undo, because a misswipe should not end a good run
 *   - the win at 2048 does not stop the game, it offers to continue
 *   - swipe and arrows both work, and swipe is the primary input
 * ------------------------------------------------------------------------- */
(function () {
  'use strict';

  var boardEl = document.getElementById('g2048');
  if (!boardEl) return;

  var GR = window.GameRoom || {};
  var beep = GR.beep || function () {};
  var getVar = GR.getVar || function () { return '#FFB627'; };
  var tune = GR.tune || function () {};
  var say = GR.say || function () {};

  var N = 4;
  var scoreEl = document.getElementById('tf-score');
  var bestEl = document.getElementById('tf-best');
  var statusEl = document.getElementById('tf-status');
  var statusText = document.getElementById('tf-status-text');
  var levelEl = document.getElementById('tf-level');
  var barEl = document.getElementById('tf-bar-fill');
  var barWrap = document.getElementById('tf-bar');
  var levelPop = document.getElementById('tf-levelpop');
  var boardWrap = document.querySelector('.tf-wrap');
  var undoBtn = document.getElementById('tf-undo');
  var againBtn = document.getElementById('tf-again');

  var grid, score, best, over, won, keepGoing, prev, uid = 0;
  var level, bestLevel;

  // Levels are the TILE MILESTONES, which is what a 2048 player already counts
  // in their head. The progress bar is keyed on score rather than on the board,
  // because the minimum score needed to build a tile T is exactly
  //
  //     T x (log2(T) - 1)
  //
  // -- every merge that builds it contributes its own value. That gives a
  // principled, monotonic progress measure instead of an invented one, and it
  // never goes backwards the way "highest tile" does not either.
  var MILESTONES = [128, 256, 512, 1024, 2048, 4096, 8192];

  function minScoreFor(t) { return t * (Math.log2(t) - 1); }

  function levelFor(sc) {
    var n = 0;
    for (var i = 0; i < MILESTONES.length; i++) {
      if (sc >= minScoreFor(MILESTONES[i])) n = i + 1;
    }
    return n;
  }

  function nextTarget(lv) {
    return lv < MILESTONES.length ? MILESTONES[lv] : null;
  }

  function isMilestoneLevel(n) { return n % 3 === 0; }

  function buzz(pattern) {
    try { if (navigator.vibrate) navigator.vibrate(pattern); } catch (e) {}
  }

  // Exposed for tests: pure functions, no state, no DOM.
  window.G2048Math = {
    minScoreFor: minScoreFor, levelFor: levelFor, nextTarget: nextTarget
  };

  try { best = parseInt(localStorage.getItem('g2048_best') || '0', 10) || 0; }
  catch (e) { best = 0; }
  // Best level survives every lost run: a hard game feels spent, not wasted.
  try { bestLevel = parseInt(localStorage.getItem('g2048_best_level') || '0', 10) || 0; }
  catch (e) { bestLevel = 0; }

  function blank() {
    return Array.from({ length: N }, function () { return Array(N).fill(null); });
  }

  function empties() {
    var out = [];
    for (var r = 0; r < N; r++) for (var c = 0; c < N; c++) if (!grid[r][c]) out.push([r, c]);
    return out;
  }

  function spawn() {
    var e = empties();
    if (!e.length) return;
    var p = e[(Math.random() * e.length) | 0];
    // 90/10 is the original's distribution. A higher 4-rate makes the game
    // meaningfully harder and feels arbitrary to a casual player.
    grid[p[0]][p[1]] = { v: Math.random() < 0.9 ? 2 : 4, id: ++uid, born: true };
  }

  function reset() {
    grid = blank();
    score = 0; over = false; won = false; keepGoing = false; prev = null;
    spawn(); spawn();
    statusEl.classList.remove('over');
    level = 0;
    statusText.textContent = 'Join the tiles to reach 2048';
    undoBtn.disabled = true;
    render();
  }

  function snapshot() {
    return {
      grid: grid.map(function (row) {
        return row.map(function (t) { return t ? { v: t.v, id: t.id } : null; });
      }),
      score: score
    };
  }

  function levelUp() {
    if (level > bestLevel) {
      bestLevel = level;
      try { localStorage.setItem('g2048_best_level', String(bestLevel)); } catch (e) {}
    }
    var big = isMilestoneLevel(level);
    var tile = MILESTONES[level - 1];

    if (levelPop) {
      levelPop.textContent = tile ? String(tile) + '!' : 'Level ' + level;
      levelPop.classList.remove('show', 'big');
      void levelPop.offsetWidth;
      levelPop.classList.add('show');
      if (big) levelPop.classList.add('big');
    }
    if (boardWrap) {
      boardWrap.classList.remove('sn-levelup');
      void boardWrap.offsetWidth;
      boardWrap.classList.add('sn-levelup');
    }
    var notes = big ? [660, 830, 990, 1320] : [720, 960];
    notes.forEach(function (hz, i) {
      setTimeout(function () { beep(hz, big ? 0.11 : 0.1, 'square', 0.14); }, i * 85);
    });
    buzz(big ? [40, 60, 40, 60, 90] : 35);
    if (big) (GR.confetti || function () {})(getVar('--gold'));
  }

  function renderLevel() {
    if (levelEl) levelEl.textContent = level;
    if (!barEl) return;
    var target = nextTarget(level);
    if (target === null) {
      barEl.style.width = '100%';
      barEl.classList.remove('near');
      if (barWrap) barWrap.setAttribute('aria-label', 'All milestones reached');
      return;
    }
    var from = level > 0 ? minScoreFor(MILESTONES[level - 1]) : 0;
    var to = minScoreFor(target);
    var pct = to > from ? Math.max(0, Math.min(100, (score - from) / (to - from) * 100)) : 0;
    barEl.style.width = pct + '%';
    barEl.classList.toggle('near', pct >= 70 && pct < 100);
    if (barWrap) {
      barWrap.setAttribute('aria-valuenow', String(Math.round(score - from)));
      barWrap.setAttribute('aria-valuemax', String(Math.round(to - from)));
      barWrap.setAttribute('aria-label',
        'Next milestone ' + target + ': ' + Math.round(pct) + '% of the way');
    }
  }

  function render() {
    boardEl.innerHTML = '';
    for (var r = 0; r < N; r++) {
      for (var c = 0; c < N; c++) {
        var cell = document.createElement('div');
        cell.className = 'tf-cell';
        boardEl.appendChild(cell);
      }
    }
    for (var r2 = 0; r2 < N; r2++) {
      for (var c2 = 0; c2 < N; c2++) {
        var t = grid[r2][c2];
        if (!t) continue;
        var el = document.createElement('div');
        el.className = 'tf-tile v' + (t.v > 2048 ? 'max' : t.v) +
          (t.born ? ' born' : '') + (t.merged ? ' merged' : '');
        el.style.setProperty('--r', r2);
        el.style.setProperty('--c', c2);
        el.textContent = t.v;
        boardEl.appendChild(el);
        t.born = false; t.merged = false;
      }
    }
    scoreEl.textContent = score;
    bestEl.textContent = best;
    renderLevel();
  }

  // Collapse one line toward index 0. Returns the new line and points scored.
  function collapse(line) {
    var vals = line.filter(Boolean);
    var out = [], gained = 0;
    for (var i = 0; i < vals.length; i++) {
      if (i + 1 < vals.length && vals[i].v === vals[i + 1].v) {
        var v = vals[i].v * 2;
        out.push({ v: v, id: vals[i].id, merged: true });
        gained += v;
        if (v === 2048 && !won) won = true;
        i++;                       // a tile merges at most once per move
      } else {
        out.push(vals[i]);
      }
    }
    while (out.length < N) out.push(null);
    return { line: out, gained: gained };
  }

  function lines(dir) {
    var out = [];
    for (var i = 0; i < N; i++) {
      var line = [];
      for (var j = 0; j < N; j++) {
        if (dir === 'left') line.push(grid[i][j]);
        else if (dir === 'right') line.push(grid[i][N - 1 - j]);
        else if (dir === 'up') line.push(grid[j][i]);
        else line.push(grid[N - 1 - j][i]);
      }
      out.push(line);
    }
    return out;
  }

  function write(dir, i, line) {
    for (var j = 0; j < N; j++) {
      if (dir === 'left') grid[i][j] = line[j];
      else if (dir === 'right') grid[i][N - 1 - j] = line[j];
      else if (dir === 'up') grid[j][i] = line[j];
      else grid[N - 1 - j][i] = line[j];
    }
  }

  function move(dir) {
    if (over) return;
    var before = JSON.stringify(grid.map(function (r) {
      return r.map(function (t) { return t ? t.v : 0; });
    }));
    var snap = snapshot();

    var gained = 0;
    lines(dir).forEach(function (line, i) {
      var res = collapse(line);
      gained += res.gained;
      write(dir, i, res.line);
    });

    var after = JSON.stringify(grid.map(function (r) {
      return r.map(function (t) { return t ? t.v : 0; });
    }));
    if (before === after) return;      // nothing moved: not a turn, no new tile

    prev = snap;
    undoBtn.disabled = false;
    score += gained;

    // Level up on crossing a milestone's minimum score.
    var lv = levelFor(score);
    if (lv > level) {
      level = lv;
      levelUp();
    }
    if (gained) beep(320 + Math.min(gained, 600), 0.07, 'square', 0.10);
    if (score > best) {
      best = score;
      try { localStorage.setItem('g2048_best', String(best)); } catch (e) {}
    }

    spawn();
    render();

    if (won && !keepGoing) {
      statusEl.classList.add('over');
      statusText.textContent = 'You made 2048. Keep going?';
      say(statusText.textContent);
      tune([523, 659, 784, 1047], 0.1);
      (GR.confetti || function () {})((GR.getVar || function () { return '#FFB627'; })('--gold'));
      keepGoing = true;
      window.AdLab && AdLab.track('game_end', { game: '2048', outcome: 'win', score: score });
      return;
    }
    if (!canMove()) end();
  }

  function canMove() {
    if (empties().length) return true;
    for (var r = 0; r < N; r++) {
      for (var c = 0; c < N; c++) {
        var v = grid[r][c].v;
        if (c + 1 < N && grid[r][c + 1].v === v) return true;
        if (r + 1 < N && grid[r + 1][c].v === v) return true;
      }
    }
    return false;
  }

  function end() {
    over = true;
    statusEl.classList.add('over');
    statusText.textContent = 'No moves left — ' + score + ' points';
    say(statusText.textContent);
    tune([330, 262], 0.16);
    window.AdLab && AdLab.track('game_end', { game: '2048', outcome: 'lose', score: score });
  }

  function undo() {
    if (!prev) return;
    grid = prev.grid.map(function (row) {
      return row.map(function (t) { return t ? { v: t.v, id: t.id } : null; });
    });
    score = prev.score;
    prev = null;
    over = false;
    undoBtn.disabled = true;
    statusEl.classList.remove('over');
    level = 0;
    statusText.textContent = 'Join the tiles to reach 2048';
    render();
  }

  var KEYS = {
    ArrowLeft: 'left', ArrowRight: 'right', ArrowUp: 'up', ArrowDown: 'down',
    a: 'left', d: 'right', w: 'up', s: 'down',
    A: 'left', D: 'right', W: 'up', S: 'down'
  };

  window.addEventListener('keydown', function (e) {
    var d = KEYS[e.key];
    if (!d) return;
    e.preventDefault();
    if (!started) { started = true; window.AdLab && AdLab.track('game_start', { game: '2048', mode: 'solo' }); }
    move(d);
  });

  var started = false;
  var t0 = null;
  boardEl.addEventListener('touchstart', function (e) {
    t0 = { x: e.touches[0].clientX, y: e.touches[0].clientY };
  }, { passive: true });

  boardEl.addEventListener('touchend', function (e) {
    if (!t0) return;
    var dx = e.changedTouches[0].clientX - t0.x, dy = e.changedTouches[0].clientY - t0.y;
    t0 = null;
    if (Math.abs(dx) < 24 && Math.abs(dy) < 24) return;
    if (!started) { started = true; window.AdLab && AdLab.track('game_start', { game: '2048', mode: 'solo' }); }
    move(Math.abs(dx) > Math.abs(dy) ? (dx > 0 ? 'right' : 'left') : (dy > 0 ? 'down' : 'up'));
  }, { passive: true });

  undoBtn.addEventListener('click', undo);
  againBtn.addEventListener('click', function () {
    window.AdLab && AdLab.track('game_replay', { game: '2048' });
    started = false;
    reset();
  });

  reset();
})();
