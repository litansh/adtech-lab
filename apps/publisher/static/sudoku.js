/* Sudoku.
 *
 * The generator is the interesting part. Producing a *valid* grid is easy;
 * producing one with exactly ONE solution is the requirement, and it is what
 * separates a real puzzle from a grid with blanks. A puzzle with two solutions
 * is unsolvable by logic -- the player must guess, discovers the guess was
 * arbitrary, and concludes the game is broken rather than that they are.
 *
 * So: build a full grid, then remove cells one at a time, and only keep a
 * removal if the puzzle still solves uniquely. That costs a solve per attempted
 * removal, which is why the counting solver stops at two solutions -- we never
 * need to know it has nine, only that it has more than one.
 */
(function () {
  var boardEl = document.getElementById('sudoku');

  var GR = window.GameRoom || {};
  var beep = GR.beep || function () {};
  var say = GR.say || function () {};

  var padEl = document.getElementById('sd-pad');
  var statusText = document.getElementById('sd-status-text');
  var statusEl = document.getElementById('sd-status');
  var againBtn = document.getElementById('sd-again');
  var mistakesEl = document.getElementById('sd-mistakes');
  var levelsEl = document.getElementById('sd-levels');

  // Givens per difficulty. Fewer givens is harder, but the honest driver of
  // difficulty is which TECHNIQUES are needed, not the count -- so these are
  // named Gentle/Steady/Tricky rather than Easy/Medium/Hard, which would
  // promise a precision the generator does not deliver.
  var GIVENS = { gentle: 45, steady: 36, tricky: 30 };
  var difficulty = 'gentle';

  var solution, puzzle, grid, selected = -1, mistakes = 0, done = false;

  function idx(r, c) { return r * 9 + c; }

  function canPlace(g, i, v) {
    var r = (i / 9) | 0, c = i % 9;
    for (var k = 0; k < 9; k++) {
      if (g[idx(r, k)] === v && k !== c) return false;
      if (g[idx(k, c)] === v && k !== r) return false;
    }
    var br = ((r / 3) | 0) * 3, bc = ((c / 3) | 0) * 3;
    for (var a = 0; a < 3; a++) {
      for (var b = 0; b < 3; b++) {
        var j = idx(br + a, bc + b);
        if (g[j] === v && j !== i) return false;
      }
    }
    return true;
  }

  // The RNG is injectable so the same generator can produce a RANDOM puzzle
  // here and a SEEDED one for the daily. A daily puzzle that is not identical
  // for every player is not a daily puzzle -- the whole mechanic is that the
  // result is comparable.
  var rnd = Math.random;

  function shuffled() {
    var a = [1, 2, 3, 4, 5, 6, 7, 8, 9];
    for (var i = a.length - 1; i > 0; i--) {
      var j = (rnd() * (i + 1)) | 0;
      var t = a[i]; a[i] = a[j]; a[j] = t;
    }
    return a;
  }

  function fill(g, pos) {
    if (pos === 81) return true;
    if (g[pos] !== 0) return fill(g, pos + 1);
    var vals = shuffled();
    for (var i = 0; i < 9; i++) {
      if (canPlace(g, pos, vals[i])) {
        g[pos] = vals[i];
        if (fill(g, pos + 1)) return true;
        g[pos] = 0;
      }
    }
    return false;
  }

  // Counts solutions but stops at `limit`. We never need to know a puzzle has
  // nine solutions, only that it has more than one.
  function countSolutions(g, limit) {
    var first = -1;
    for (var i = 0; i < 81; i++) if (g[i] === 0) { first = i; break; }
    if (first === -1) return 1;
    var n = 0;
    for (var v = 1; v <= 9; v++) {
      if (canPlace(g, first, v)) {
        g[first] = v;
        n += countSolutions(g, limit - n);
        g[first] = 0;
        if (n >= limit) return n;
      }
    }
    return n;
  }

  function generate(givens) {
    var full = new Array(81).fill(0);
    fill(full, 0);
    var p = full.slice();

    var order = [];
    for (var i = 0; i < 81; i++) order.push(i);
    for (var k = order.length - 1; k > 0; k--) {
      var j = (rnd() * (k + 1)) | 0;
      var t = order[k]; order[k] = order[j]; order[j] = t;
    }

    var remaining = 81;
    for (var n = 0; n < order.length && remaining > givens; n++) {
      var pos = order[n], keep = p[pos];
      p[pos] = 0;
      if (countSolutions(p.slice(), 2) !== 1) {
        p[pos] = keep;           // removing it made the puzzle ambiguous
      } else {
        remaining--;
      }
    }
    return { solution: full, puzzle: p };
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

  function reset() {
    var g = generate(GIVENS[difficulty]);
    solution = g.solution;
    puzzle = g.puzzle;
    grid = puzzle.slice();
    selected = -1; mistakes = 0; done = false;
    statusEl.classList.remove('over');
    statusText.textContent = 'Fill every row, column and box with 1-9';
    render();
    window.AdLab && AdLab.track('game_start', { game: 'sudoku', level: difficulty });
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
      // Highlighting the same number is the single biggest accessibility win
      // in Sudoku: it turns a scanning task into a looking task.
      el.classList.toggle('same', selVal !== 0 && v === selVal && i !== selected);
      el.classList.toggle('peer', selected >= 0 && isPeer(i, selected));
    }
    if (mistakesEl) mistakesEl.textContent = mistakes;
  }

  function isPeer(a, b) {
    if (a === b) return false;
    var ra = (a / 9) | 0, ca = a % 9, rb = (b / 9) | 0, cb = b % 9;
    if (ra === rb || ca === cb) return true;
    return ((ra / 3) | 0) === ((rb / 3) | 0) && ((ca / 3) | 0) === ((cb / 3) | 0);
  }

  function place(v) {
    if (done || selected < 0 || puzzle[selected] !== 0) return;
    if (v === 0) { grid[selected] = 0; render(); return; }

    grid[selected] = v;
    if (v !== solution[selected]) {
      mistakes++;
      beep(180, 0.12, 'sawtooth', 0.1);
      say('Not quite');
    } else {
      beep(720, 0.06, 'square', 0.1);
    }
    render();

    for (var i = 0; i < 81; i++) if (grid[i] !== solution[i]) return;
    win();
  }

  function win() {
    done = true;
    statusEl.classList.add('over');
    statusText.textContent = mistakes === 0
      ? 'Solved with no mistakes!'
      : 'Solved — ' + mistakes + (mistakes === 1 ? ' mistake' : ' mistakes');
    (GR.confetti || function () {})();
    (GR.tune || function () {})('win');
    window.AdLab && AdLab.track('game_end', {
      game: 'sudoku', outcome: 'win', level: difficulty, mistakes: mistakes
    });
  }

  // Exported before the guard below, so the daily page can reuse the generator
  // without duplicating it and without needing the interactive board. Pure
  // functions only: the RNG is injected, so a seeded call cannot disturb the
  // interactive game's state.
  window.SudokuGen = {
    generate: function (givens, rng) {
      var prev = rnd;
      rnd = rng || Math.random;
      try { return generate(givens); } finally { rnd = prev; }
    },
    GIVENS: GIVENS
  };

  // Everything below needs a board. The daily page has its own.
  if (!boardEl) return;

  boardEl.addEventListener('click', function (e) {
    var c = e.target.closest('.sd-cell');
    if (!c) return;
    selected = +c.dataset.i;
    render();
  });

  if (padEl) {
    padEl.addEventListener('click', function (e) {
      var b = e.target.closest('button');
      if (!b) return;
      place(+b.dataset.v);
    });
  }

  document.addEventListener('keydown', function (e) {
    if (e.key >= '1' && e.key <= '9') { place(+e.key); return; }
    if (e.key === 'Backspace' || e.key === 'Delete' || e.key === '0') { place(0); return; }
    if (selected < 0) selected = 0;
    var r = (selected / 9) | 0, c = selected % 9, moved = true;
    if (e.key === 'ArrowLeft') c = (c + 8) % 9;
    else if (e.key === 'ArrowRight') c = (c + 1) % 9;
    else if (e.key === 'ArrowUp') r = (r + 8) % 9;
    else if (e.key === 'ArrowDown') r = (r + 1) % 9;
    else moved = false;
    if (moved) { e.preventDefault(); selected = idx(r, c); render(); }
  });

  if (levelsEl) {
    [].forEach.call(levelsEl.querySelectorAll('.chip'), function (b) {
      b.addEventListener('click', function () {
        difficulty = b.dataset.level;
        [].forEach.call(levelsEl.querySelectorAll('.chip'), function (o) {
          o.setAttribute('aria-pressed', String(o === b));
        });
        reset();
      });
    });
  }

  if (againBtn) {
    againBtn.addEventListener('click', function () {
      window.AdLab && AdLab.track('game_replay', { game: 'sudoku', level: difficulty });
      reset();
    });
  }

  build();
  reset();
})();
