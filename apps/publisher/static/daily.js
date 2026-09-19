/* The Daily Puzzle.
 *
 * docs/audience.md argues this is the highest-leverage thing on the site, and
 * the reasoning is worth keeping next to the code:
 *
 *   Right now there is no sentence that finishes "I play at xoxoxo.live instead
 *   of Google's version because...". Until there is, every acquisition channel
 *   is a leaky bucket -- traffic arrives, plays once, and leaves.
 *
 * A daily puzzle supplies the missing half in two ways, and they are different:
 *
 *   RETENTION     one puzzle, the same for everyone, once a day. A reason to
 *                 come back tomorrow that does not depend on us sending
 *                 anything or knowing who you are.
 *   ACQUISITION   a spoiler-free shareable result. This is the part that
 *                 actually grows a site with no budget: users bring users.
 *
 * The share format is deliberately Wordle-shaped, because the shape is what
 * worked, not the word game. It must be:
 *   - spoiler-free    it cannot reveal the solution
 *   - comparable      two people's results must be worth comparing
 *   - visual          a grid of blocks travels further than a sentence
 *
 * Ours is a 3x3 of the Sudoku's own boxes: filled where you solved it cleanly,
 * marked where you made a mistake. It says how you did without saying anything
 * about the puzzle.
 */
(function () {
  var boardEl = document.getElementById('daily-board');
  if (!boardEl || !window.SudokuGen) return;

  var GR = window.GameRoom || {};
  var beep = GR.beep || function () {};

  var statusEl = document.getElementById('dy-status');
  var statusText = document.getElementById('dy-status-text');
  var padEl = document.getElementById('dy-pad');
  var timerEl = document.getElementById('dy-timer');
  var streakEl = document.getElementById('dy-streak');
  var shareBtn = document.getElementById('dy-share');
  var numberEl = document.getElementById('dy-number');

  // The epoch is fixed so the puzzle number is stable forever. Changing it
  // renumbers every past result anybody has shared.
  var EPOCH = Date.UTC(2026, 0, 1);

  function todayKey() {
    var n = new Date();
    return n.getUTCFullYear() + '-' +
           String(n.getUTCMonth() + 1).padStart(2, '0') + '-' +
           String(n.getUTCDate()).padStart(2, '0');
  }

  function puzzleNumber(key) {
    var p = key.split('-');
    var t = Date.UTC(+p[0], +p[1] - 1, +p[2]);
    return Math.floor((t - EPOCH) / 86400000) + 1;
  }

  // mulberry32: small, fast, and -- the only property that matters here --
  // identical on every device. Math.random is not seedable, so it cannot
  // produce the same puzzle for two people.
  function seededRng(seed) {
    var a = seed >>> 0;
    return function () {
      a = (a + 0x6D2B79F5) >>> 0;
      var t = Math.imul(a ^ (a >>> 15), 1 | a);
      t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
  }

  function hashString(s) {
    var h = 2166136261 >>> 0;
    for (var i = 0; i < s.length; i++) {
      h ^= s.charCodeAt(i);
      h = Math.imul(h, 16777619) >>> 0;
    }
    return h >>> 0;
  }

  // ---- Which day are we playing? ----------------------------------------
  //
  // The archive. Every past puzzle is reproducible from its date alone --
  // the generator is seeded, so nothing is stored and nothing needs to be.
  //
  // It exists because a visitor arriving on the day of a Reddit post finds
  // exactly one puzzle. Twenty is a reason to stay; one is a reason to leave.
  // The cost of holding twenty is zero, which is unusual enough to take.

  function keyFromDate(d) {
    return d.getUTCFullYear() + '-' +
           String(d.getUTCMonth() + 1).padStart(2, '0') + '-' +
           String(d.getUTCDate()).padStart(2, '0');
  }

  function shiftKey(k, days) {
    var p = k.split('-');
    return keyFromDate(new Date(Date.UTC(+p[0], +p[1] - 1, +p[2] + days)));
  }

  /* Clamped at both ends. A future date would hand out tomorrow's puzzle to
   * anyone who edited the URL, which breaks the only promise the daily makes:
   * that everyone is solving the same one today. Before the epoch there is
   * nothing to generate. */
  function requestedKey() {
    var today = todayKey();
    var want;
    try {
      want = new URLSearchParams(location.search).get('d');
    } catch (e) { want = null; }
    if (!want || !/^\d{4}-\d{2}-\d{2}$/.test(want)) return today;

    var p = want.split('-');
    var t = Date.UTC(+p[0], +p[1] - 1, +p[2]);
    if (isNaN(t) || t > Date.parse(today + 'T00:00:00Z')) return today;
    if (t < EPOCH) return keyFromDate(new Date(EPOCH));
    return want;
  }

  var key = requestedKey();
  var isToday = key === todayKey();
  var num = puzzleNumber(key);
  var solution, puzzle, grid, selected = -1, done = false;
  var boxMistakes = new Array(9).fill(0);
  var startedAt = 0, elapsed = 0, ticking = null, started = false;

  function stateKey() { return 'daily_' + key; }

  function loadState() {
    try { return JSON.parse(localStorage.getItem(stateKey())) || null; }
    catch (e) { return null; }
  }

  function saveState(extra) {
    try {
      localStorage.setItem(stateKey(), JSON.stringify(Object.assign({
        grid: grid, mistakes: boxMistakes, elapsed: elapsed, done: done
      }, extra || {})));
    } catch (e) {}
  }

  function readStreak() {
    try { return JSON.parse(localStorage.getItem('daily_streak')) || { n: 0, last: '' }; }
    catch (e) { return { n: 0, last: '' }; }
  }

  function bumpStreak() {
    var st = readStreak();
    // Archive puzzles never touch the streak. A streak that can be run up by
    // playing last week is not a streak, it is a total -- and the number stops
    // meaning "you came back", which is the only thing it was for.
    if (!isToday) return st;
    if (st.last === key) return st;
    var y = new Date(Date.now() - 86400000);
    var yKey = y.getUTCFullYear() + '-' + String(y.getUTCMonth() + 1).padStart(2, '0') +
               '-' + String(y.getUTCDate()).padStart(2, '0');
    // A missed day resets to one rather than to zero: today still counts.
    st = { n: st.last === yKey ? st.n + 1 : 1, last: key };
    try { localStorage.setItem('daily_streak', JSON.stringify(st)); } catch (e) {}
    return st;
  }

  function boxOf(i) {
    var r = (i / 9) | 0, c = i % 9;
    return ((r / 3) | 0) * 3 + ((c / 3) | 0);
  }

  function fmt(ms) {
    var s = Math.floor(ms / 1000);
    return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
  }

  function tick() {
    if (done) return;
    elapsed = Date.now() - startedAt;
    timerEl.textContent = fmt(elapsed);
  }

  function build() {
    boardEl.innerHTML = '';
    for (var i = 0; i < 81; i++) {
      var cell = document.createElement('button');
      cell.className = 'sd-cell';
      cell.dataset.i = i;
      var r = (i / 9) | 0, c = i % 9;
      if (c % 3 === 2 && c !== 8) cell.classList.add('br');
      if (r % 3 === 2 && r !== 8) cell.classList.add('bb');
      cell.setAttribute('aria-label', 'Row ' + (r + 1) + ' column ' + (c + 1));
      boardEl.appendChild(cell);
    }
  }

  function render() {
    var cells = boardEl.children;
    var selVal = selected >= 0 ? grid[selected] : 0;
    for (var i = 0; i < 81; i++) {
      var el = cells[i], v = grid[i];
      el.textContent = v || '';
      el.classList.toggle('given', puzzle[i] !== 0);
      el.classList.toggle('sel', i === selected);
      el.classList.toggle('wrong', v !== 0 && puzzle[i] === 0 && v !== solution[i]);
      el.classList.toggle('same', selVal !== 0 && v === selVal && i !== selected);
    }
    var st = readStreak();
    if (streakEl) streakEl.textContent = st.n;
  }

  function place(v) {
    if (done || selected < 0 || puzzle[selected] !== 0) return;
    if (!started) {
      started = true;
      startedAt = Date.now() - elapsed;
      ticking = setInterval(tick, 500);
      window.AdLab && AdLab.track('game_start', { game: 'daily', puzzle: num });
    }
    if (v === 0) { grid[selected] = 0; render(); saveState(); return; }

    grid[selected] = v;
    if (v !== solution[selected]) {
      boxMistakes[boxOf(selected)]++;
      beep(180, 0.12, 'sawtooth', 0.1);
    } else {
      beep(720, 0.06, 'square', 0.1);
    }
    render();
    saveState();

    for (var i = 0; i < 81; i++) if (grid[i] !== solution[i]) return;
    finish();
  }

  function finish() {
    done = true;
    if (ticking) clearInterval(ticking);
    elapsed = started ? Date.now() - startedAt : elapsed;
    var st = bumpStreak();
    saveState();
    statusEl.classList.add('over');
    var total = boxMistakes.reduce(function (a, b) { return a + b; }, 0);
    statusText.textContent = total === 0
      ? 'Solved in ' + fmt(elapsed) + ' with no mistakes'
      : 'Solved in ' + fmt(elapsed) + ' — ' + total + (total === 1 ? ' mistake' : ' mistakes');
    if (shareBtn) shareBtn.hidden = false;
    render();
    (GR.confetti || function () {})();
    (GR.tune || function () {})('win');
    window.AdLab && AdLab.track('game_end', {
      game: 'daily', outcome: 'win', puzzle: num,
      seconds: Math.round(elapsed / 1000), mistakes: total, streak: st.n
    });
  }

  // Spoiler-free: the grid shows WHERE you struggled, never what the answers
  // were. Two people can compare results without either spoiling the puzzle.
  function shareText() {
    var rows = [];
    for (var r = 0; r < 3; r++) {
      var line = '';
      for (var c = 0; c < 3; c++) {
        var m = boxMistakes[r * 3 + c];
        line += m === 0 ? '🟩' : (m <= 2 ? '🟨' : '🟥');
      }
      rows.push(line);
    }
    var total = boxMistakes.reduce(function (a, b) { return a + b; }, 0);
    return 'Sudoku #' + num + '\n' + rows.join('\n') + '\n' +
           fmt(elapsed) + (total ? ' · ' + total + ' mistakes' : ' · clean') +
           '\nxoxoxo.live/daily';
  }

  if (shareBtn) {
    shareBtn.addEventListener('click', function () {
      var text = shareText();
      window.AdLab && AdLab.track('daily_share', { puzzle: num });
      if (navigator.share) {
        navigator.share({ text: text }).catch(function () {});
        return;
      }
      // Clipboard is the fallback, and the button says what happened. A share
      // button that silently does nothing is worse than no share button.
      var done2 = function () {
        shareBtn.textContent = 'Copied!';
        setTimeout(function () { shareBtn.textContent = 'Share result'; }, 1800);
      };
      if (navigator.clipboard) navigator.clipboard.writeText(text).then(done2, function () {});
      else done2();
    });
  }

  boardEl.addEventListener('click', function (e) {
    var c = e.target.closest('.sd-cell');
    if (!c) return;
    selected = +c.dataset.i;
    render();
  });

  if (padEl) {
    padEl.addEventListener('click', function (e) {
      var b = e.target.closest('button');
      if (b) place(+b.dataset.v);
    });
  }

  document.addEventListener('keydown', function (e) {
    if (e.key >= '1' && e.key <= '9') { place(+e.key); return; }
    if (e.key === 'Backspace' || e.key === 'Delete' || e.key === '0') place(0);
  });

  // --- boot ---
  var g = window.SudokuGen.generate(38, seededRng(hashString('sudoku-' + key)));
  solution = g.solution;
  puzzle = g.puzzle;
  grid = puzzle.slice();

  var saved = loadState();
  if (saved && saved.grid && saved.grid.length === 81) {
    grid = saved.grid;
    boxMistakes = saved.mistakes || boxMistakes;
    elapsed = saved.elapsed || 0;
    done = !!saved.done;
  }

  if (numberEl) numberEl.textContent = '#' + num;

  // ---- Archive navigation -----------------------------------------------
  (function () {
    var prev = document.getElementById('dy-prev');
    var next = document.getElementById('dy-next');
    var band = document.getElementById('dy-archive');
    var when = document.getElementById('dy-archive-date');

    var earliest = keyFromDate(new Date(EPOCH));
    if (prev) {
      if (key > earliest) {
        prev.href = '/daily?d=' + shiftKey(key, -1);
      } else {
        prev.hidden = true;   // nothing before the epoch to generate
      }
    }
    if (next) {
      // No link forward from today. Tomorrow's puzzle is not ours to hand out
      // early -- the daily's only promise is that everyone has the same one.
      if (isToday) next.hidden = true;
      else next.href = '/daily?d=' + shiftKey(key, 1);
    }

    if (!isToday && band && when) {
      var p = key.split('-');
      var d = new Date(Date.UTC(+p[0], +p[1] - 1, +p[2]));
      when.textContent = d.toLocaleDateString(undefined, {
        timeZone: 'UTC', weekday: 'long', day: 'numeric', month: 'long'
      });
      band.hidden = false;
      // Said plainly, because a streak that does not move needs explaining
      // before someone decides it is broken.
      when.title = 'Archive puzzles do not count towards your streak.';
    }
  })();
  build();
  render();
  timerEl.textContent = fmt(elapsed);

  if (done) {
    statusEl.classList.add('over');
    var total = boxMistakes.reduce(function (a, b) { return a + b; }, 0);
    statusText.textContent = 'Done today in ' + fmt(elapsed) +
      (total ? ' · ' + total + ' mistakes' : ' · no mistakes') + '. New puzzle at midnight UTC.';
    if (shareBtn) shareBtn.hidden = false;
  }
})();
