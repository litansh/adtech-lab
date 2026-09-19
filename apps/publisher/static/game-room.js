(() => {
  /* ================= Near-miss feedback =================
   *
   * Tell a player how close they came. Proposed by the Product agent (#56) and
   * ranked first on evidence: Connections' "one away" is repeatedly named as
   * the reason a loss still feels like information rather than a full stop.
   *
   * It lives HERE rather than in adlab.js because adlab.js is analytics and is
   * stripped from the itch.io builds -- which is exactly where a new player
   * meets these games for the first time. A retention feature that only works
   * for people who already found the site is not a retention feature.
   *
   * It also never moves the replay button: the line is rendered BELOW the
   * actions. docs/ad-placement.md rules out unconditionally anything that
   * delays an action the player has already decided to take, and "play again"
   * is the only engagement signal we have.
   */
  var KEY = 'adlab_records_v1';

  /* What "better" means, per game. Not every game agrees: Memory and the daily
   * are races, where LOWER is better, and treating them like a score is how a
   * personal best ends up recording someone's worst round. */
  var SCORING = {
    '2048':  { field: 'score',   better: 'higher', unit: 'point'  },
    'snake': { field: 'score',   better: 'higher', unit: 'point'  },
    'memory':{ field: 'moves',   better: 'lower',  unit: 'move'   },
    'daily': { field: 'seconds', better: 'lower',  unit: 'second' }
  };
  /* Tic-tac-toe and Connect Four are deliberately absent. "How close" in an
   * adversarial game means "was there a winning move you missed", which needs
   * real position analysis -- a different and much larger piece of work than
   * this, and saying nothing is better than saying something vague. */

  function readRecords() {
    try { return JSON.parse(localStorage.getItem(KEY)) || {}; }
    catch (e) { return {}; }
  }

  function plural(n, unit) { return n + ' ' + unit + (n === 1 ? '' : 's'); }

  /* message returns the line to show, or '' when there is nothing honest to
   * say -- a first game has no near miss, and inventing encouragement for it
   * would make the real message worth less. */
  function message(ev, prevBest) {
    var rule = SCORING[ev && ev.game];
    if (!rule) return '';
    var value = Number(ev[rule.field]);
    if (!isFinite(value)) return '';

    if (prevBest === null || prevBest === undefined || !isFinite(prevBest)) {
      return '';                         /* nothing to be close to yet */
    }

    var beat = rule.better === 'higher' ? value > prevBest : value < prevBest;
    if (beat) {
      return 'New best — ' + plural(value, rule.unit) + '.';
    }
    var gap = Math.abs(value - prevBest);
    if (gap === 0) return 'You matched your best exactly.';
    return plural(gap, rule.unit) + ' from your best of ' + prevBest + '.';
  }

  function render(text) {
    var el = document.getElementById('near-miss');
    if (!el) return;
    if (!text) { el.hidden = true; el.textContent = ''; return; }
    el.textContent = text;
    el.hidden = false;
    /* Announced, because the whole point is that a loss carries information,
     * and information nobody can hear is decoration. */
    var live = document.getElementById('live');
    if (live) live.textContent = text;
  }

  function bestFor(game) {
    var rule = SCORING[game];
    if (!rule) return null;
    var r = readRecords()[game];
    if (!r || !isFinite(Number(r.best))) return null;
    return Number(r.best);
  }

  function handle(ev) {
    try { render(message(ev, bestFor(ev && ev.game))); } catch (e) {}
  }

  /* Wrapping AdLab.track rather than being called by six games: the previous
   * best has to be read BEFORE adlab.js records the new one, and wrapping is
   * the only place that ordering is guaranteed. game-room.js loads after
   * adlab.js, so the wrap is always in place before the first game ends. */
  if (window.AdLab && typeof window.AdLab.track === 'function') {
    var original = window.AdLab.track;
    window.AdLab.track = function (name, props) {
      if (name === 'game_end') handle(props);
      return original.apply(window.AdLab, arguments);
    };
  } else {
    /* itch.io: adlab.js is stripped, so nothing exists to wrap and nothing
     * writes a record either. A minimal stand-in keeps the games' existing
     * `window.AdLab && AdLab.track(...)` calls working, stores the best
     * locally, and sends nothing anywhere -- an itch build must remain silent. */
    window.AdLab = {
      track: function (name, props) {
        if (name !== 'game_end' || !props || !props.game) return;
        handle(props);
        var rule = SCORING[props.game];
        if (!rule) return;
        var value = Number(props[rule.field]);
        if (!isFinite(value)) return;
        try {
          var all = readRecords(), r = all[props.game] || { played: 0, won: 0, best: null };
          r.played++;
          if (props.outcome === 'win') r.won++;
          var better = r.best === null || !isFinite(Number(r.best)) ||
            (rule.better === 'higher' ? value > r.best : value < r.best);
          if (better) r.best = value;
          all[props.game] = r;
          localStorage.setItem(KEY, JSON.stringify(all));
        } catch (e) {}
      }
    };
  }

  /* Exposed for the browser suite, which asserts the wording rather than
   * driving four games to a loss. */
  window.NearMiss = { message: message, scoring: SCORING };
})();

(() => {
  /* ================= Shared helpers ================= */
  const liveEl = document.getElementById('live');
  const getVar = n => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
  let soundOn = true;
  let actx = null;

  function beep(freq, dur = .12, type = 'triangle', vol = .18){
    if (!soundOn) return;
    try{
      actx = actx || new (window.AudioContext || window.webkitAudioContext)();
      if (actx.state === 'suspended') actx.resume();
      const o = actx.createOscillator(), g = actx.createGain();
      o.type = type; o.frequency.value = freq;
      g.gain.setValueAtTime(0, actx.currentTime);
      g.gain.linearRampToValueAtTime(vol, actx.currentTime + .012);
      g.gain.exponentialRampToValueAtTime(.0001, actx.currentTime + dur);
      o.connect(g).connect(actx.destination);
      o.start(); o.stop(actx.currentTime + dur + .02);
    }catch(e){/* no sound; the game carries on regardless */}
  }
  const tune = (notes, gap = .11) => notes.forEach((f, i) => setTimeout(() => beep(f, .18, 'triangle', .2), i * gap * 1000));
  const say = msg => { liveEl.textContent = msg; };

  /* ---------- Confetti ---------- */
  const cv = document.getElementById('confetti');
  const ctx = cv.getContext('2d');
  let bits = [], raf = 0;
  function fit(){ cv.width = innerWidth * devicePixelRatio; cv.height = innerHeight * devicePixelRatio; }
  fit(); addEventListener('resize', fit);

  function confetti(main){
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const colors = [main, getVar('--gold'), getVar('--o'), getVar('--x')];
    const dpr = devicePixelRatio;
    for (let i = 0; i < 110; i++){
      bits.push({
        x: innerWidth / 2 * dpr + (Math.random() - .5) * 240 * dpr,
        y: innerHeight * .45 * dpr,
        vx: (Math.random() - .5) * 11 * dpr,
        vy: (Math.random() * -13 - 4) * dpr,
        w: (5 + Math.random() * 7) * dpr,
        h: (8 + Math.random() * 9) * dpr,
        rot: Math.random() * 6.28,
        vr: (Math.random() - .5) * .3,
        c: colors[(Math.random() * colors.length) | 0],
        life: 1
      });
    }
    if (!raf) raf = requestAnimationFrame(tick);
  }

  function tick(){
    ctx.clearRect(0, 0, cv.width, cv.height);
    const g = .38 * devicePixelRatio;
    bits = bits.filter(b => b.life > 0 && b.y < cv.height + 60);
    for (const b of bits){
      b.vy += g; b.x += b.vx; b.y += b.vy; b.rot += b.vr; b.vx *= .995; b.life -= .004;
      ctx.save();
      ctx.translate(b.x, b.y);
      ctx.rotate(b.rot);
      ctx.globalAlpha = Math.max(0, Math.min(1, b.life * 1.6));
      ctx.fillStyle = b.c;
      ctx.fillRect(-b.w / 2, -b.h / 2, b.w, b.h);
      ctx.restore();
    }
    if (bits.length) raf = requestAnimationFrame(tick);
    else { raf = 0; ctx.clearRect(0, 0, cv.width, cv.height); }
  }

  /* ---------- Game switching ---------- */
  /* Games live on their own pages (/xo, /connect-four, /snake), so the old
     in-page tab switcher is gone. Navigation is plain links in the top bar.

     The shared helpers are exposed so a new game can reuse the sound, the
     confetti and the screen-reader announcer rather than reimplementing three
     slightly-different versions of each. */
  window.GameRoom = {
    beep: beep,
    tune: tune,
    confetti: confetti,
    say: say,
    getVar: getVar,
    soundOn: function(){ return soundOn; }
  };

  const soundBtn = document.getElementById('sound');
  if (soundBtn) soundBtn.addEventListener('click', () => {
    soundOn = !soundOn;
    soundBtn.setAttribute('aria-pressed', String(soundOn));
    soundBtn.textContent = soundOn ? '\u{1F50A} Sound' : '\u{1F507} Muted';
    if (soundOn) beep(660, .1);
  });

  /* ================= Game 1: Tic-Tac-Toe ================= */
  (() => {
    const LINES = [[0,1,2],[3,4,5],[6,7,8],[0,3,6],[1,4,7],[2,5,8],[0,4,8],[2,4,6]];
    const SVG_X = '<svg viewBox="0 0 100 100"><line class="mark x" x1="26" y1="26" x2="74" y2="74" pathLength="100"/><line class="mark x d2" x1="74" y1="26" x2="26" y2="74" pathLength="100"/></svg>';
    const SVG_O = '<svg viewBox="0 0 100 100"><circle class="mark o" cx="50" cy="50" r="25" pathLength="100"/></svg>';

    const boardEl = document.getElementById('board');
    if (!boardEl) return;                       // not the Tic-Tac-Toe page
    const strikeEl = document.getElementById('strike');
    const statusEl = document.getElementById('status');
    const statusText = document.getElementById('status-text');
    const statusPip = document.getElementById('status-pip');
    const nameX = document.getElementById('name-x');
    const nameO = document.getElementById('name-o');
    const scX = document.getElementById('sc-x');
    const scO = document.getElementById('sc-o');
    const levelsEl = document.getElementById('levels');

    let board = Array(9).fill('');
    let turn = 'X';
    let over = false;
    let starter = 'X';
    let vsCpu = false;
    let level = 0;               // 0 easy, 1 medium, 2 hard
    let busy = false;
    const wins = {X:0, O:0, T:0};

    const nameOf = p => (p === 'X' ? nameX.value.trim() || 'X' : nameO.value.trim() || 'O');

    for (let i = 0; i < 9; i++){
      const b = document.createElement('button');
      b.className = 'cell ghost';
      b.type = 'button';
      b.dataset.i = i;
      b.setAttribute('role', 'gridcell');
      b.setAttribute('aria-label', 'Square ' + (i + 1) + ', empty');
      b.addEventListener('click', () => play(i));
      b.addEventListener('keydown', onArrow);
      boardEl.appendChild(b);
    }
    const cells = [...boardEl.children];

    function onArrow(e){
      const map = {ArrowRight:-1, ArrowLeft:1, ArrowUp:-3, ArrowDown:3};
      if (!(e.key in map)) return;
      e.preventDefault();
      const i = +e.currentTarget.dataset.i;
      const n = i + map[e.key];
      if (Math.abs(map[e.key]) === 1 && Math.floor(n / 3) !== Math.floor(i / 3)) return;
      if (n < 0 || n > 8) return;
      cells[n].focus();
    }

    function winnerOf(b){
      for (const l of LINES){
        const [a, c, d] = l;
        if (b[a] && b[a] === b[c] && b[a] === b[d]) return {p:b[a], line:l};
      }
      return b.every(Boolean) ? {p:'T', line:null} : null;
    }

    function play(i){
      if (over || busy || board[i]) return;
      place(i, turn);
      beep(turn === 'X' ? 520 : 392, .1, 'square', .14);
      const res = winnerOf(board);
      if (res) return finish(res);
      turn = turn === 'X' ? 'O' : 'X';
      paintTurn();
      if (vsCpu && turn === 'O') cpuMove();
    }

    function place(i, p){
      board[i] = p;
      const c = cells[i];
      c.innerHTML = p === 'X' ? SVG_X : SVG_O;
      c.disabled = true;
      c.classList.remove('ghost');
      c.setAttribute('aria-label', 'Square ' + (i + 1) + ', ' + (p === 'X' ? 'X' : 'O'));
    }

    function finish(res){
      over = true;
      statusEl.classList.add('over');
      cells.forEach(c => { c.disabled = true; c.classList.remove('ghost'); });
      if (res.p === 'T'){
        wins.T++;
        statusEl.dataset.turn = '';
        statusPip.innerHTML = '';
        tell("It's a draw - nobody wins \u{1F91D}");
        tune([330, 294], .14);
      } else {
        wins[res.p]++;
        statusEl.dataset.turn = res.p;
        statusPip.innerHTML = res.p === 'X' ? SVG_X : SVG_O;
        tell(nameOf(res.p) + ' wins! \u{1F389}');
        drawStrike(res.line);
        res.line.forEach(i => cells[i].classList.add('win'));
        tune([523, 659, 784, 1047], .1);
        confetti(res.p === 'X' ? getVar('--x') : getVar('--o'));
      }
      updateScores();
      window.AdLab && AdLab.track('game_end', {
        game: 'xo', outcome: res.p === 'T' ? 'draw' : 'win',
        winner: res.p === 'T' ? null : res.p, mode: vsCpu ? 'cpu' : 'two', level: level,
        moves: board.filter(Boolean).length
      });
      starter = starter === 'X' ? 'O' : 'X';
    }

    function drawStrike(line){
      const pt = i => ({x: (i % 3) * 33.333 + 16.667, y: Math.floor(i / 3) * 33.333 + 16.667});
      const a = pt(line[0]), b = pt(line[2]);
      const dx = b.x - a.x, dy = b.y - a.y, len = Math.hypot(dx, dy);
      const ex = (dx / len) * 7, ey = (dy / len) * 7;
      strikeEl.innerHTML = '<line x1="' + (a.x - ex) + '" y1="' + (a.y - ey) +
                           '" x2="' + (b.x + ex) + '" y2="' + (b.y + ey) + '" pathLength="100"/>';
    }

    function tell(msg){ statusText.textContent = msg; say(msg); }

    function paintTurn(){
      statusEl.classList.remove('over');
      statusEl.dataset.turn = turn;
      statusPip.innerHTML = turn === 'X' ? SVG_X : SVG_O;
      const mine = vsCpu && turn === 'O';
      statusText.textContent = mine ? 'Computer is thinking…' : nameOf(turn) + "'s turn";
      cells.forEach((c, i) => {
        c.dataset.ghost = turn;
        if (!board[i]) c.disabled = !!mine;
      });
      scX.classList.toggle('active', turn === 'X');
      scO.classList.toggle('active', turn === 'O');
    }

    function updateScores(){
      document.getElementById('win-x').textContent = wins.X;
      document.getElementById('win-o').textContent = wins.O;
      document.getElementById('win-t').textContent = wins.T;
    }

    function cpuMove(){
      busy = true;
      const delay = 380 + Math.random() * 320;
      setTimeout(() => {
        const free = board.map((v, i) => v ? -1 : i).filter(i => i >= 0);
        if (!free.length || over){ busy = false; return; }
        let i;
        if (level === 2) i = best('O');
        else if (level === 1) i = Math.random() < .65 ? best('O') : free[(Math.random() * free.length) | 0];
        else i = smartish(free);
        busy = false;
        play(i);
      }, delay);
    }

    // Easy: blocks or wins only sometimes, otherwise random - so kids can win
    function smartish(free){
      if (Math.random() < .3) return best('O');
      return free[(Math.random() * free.length) | 0];
    }

    function best(me){
      const other = me === 'X' ? 'O' : 'X';
      let bestScore = -Infinity, move = -1;
      for (let i = 0; i < 9; i++){
        if (board[i]) continue;
        board[i] = me;
        const s = minimax(board, 0, false, me, other, -Infinity, Infinity);
        board[i] = '';
        if (s > bestScore){ bestScore = s; move = i; }
      }
      return move;
    }

    function minimax(b, depth, maxing, me, other, alpha, beta){
      const r = winnerOf(b);
      if (r){
        if (r.p === me) return 10 - depth;
        if (r.p === other) return depth - 10;
        return 0;
      }
      if (maxing){
        let v = -Infinity;
        for (let i = 0; i < 9; i++){
          if (b[i]) continue;
          b[i] = me;
          v = Math.max(v, minimax(b, depth + 1, false, me, other, alpha, beta));
          b[i] = '';
          alpha = Math.max(alpha, v);
          if (beta <= alpha) break;
        }
        return v;
      }
      let v = Infinity;
      for (let i = 0; i < 9; i++){
        if (b[i]) continue;
        b[i] = other;
        v = Math.min(v, minimax(b, depth + 1, true, me, other, alpha, beta));
        b[i] = '';
        beta = Math.min(beta, v);
        if (beta <= alpha) break;
      }
      return v;
    }

    function newRound(){
      window.AdLab && AdLab.track('game_start', {
        game: 'xo', mode: vsCpu ? 'cpu' : 'two', level: level
      });
      board = Array(9).fill('');
      over = false; busy = false;
      strikeEl.innerHTML = '';
      cells.forEach((c, i) => {
        c.innerHTML = '';
        c.disabled = false;
        c.classList.remove('win');
        c.classList.add('ghost');
        c.setAttribute('aria-label', 'Square ' + (i + 1) + ', empty');
      });
      turn = starter;
      paintTurn();
      if (vsCpu && turn === 'O') cpuMove();
    }

    if (matchMedia('(pointer:fine)').matches && !matchMedia('(prefers-reduced-motion: reduce)').matches){
      const wrap = document.querySelector('.board-wrap');
      wrap.addEventListener('pointermove', e => {
        const r = wrap.getBoundingClientRect();
        const px = (e.clientX - r.left) / r.width - .5;
        const py = (e.clientY - r.top) / r.height - .5;
        boardEl.style.transform = 'rotateY(' + (px * 7).toFixed(2) + 'deg) rotateX(' + (-py * 7).toFixed(2) + 'deg)';
      });
      wrap.addEventListener('pointerleave', () => { boardEl.style.transform = ''; });
    }

    const modeTwo = document.getElementById('mode-two');
    const modeCpu = document.getElementById('mode-cpu');
    function setMode(cpu){
      vsCpu = cpu;
      modeTwo.setAttribute('aria-pressed', String(!cpu));
      modeCpu.setAttribute('aria-pressed', String(cpu));
      levelsEl.hidden = !cpu;
      nameO.disabled = cpu;
      nameO.value = cpu ? 'Computer' : 'O';
      showHint();
      starter = 'X';
      newRound();
    }
    modeTwo.addEventListener('click', () => setMode(false));
    modeCpu.addEventListener('click', () => setMode(true));

    // The unbeatable level is named "Perfect" rather than "Hard", and says so.
    // A player who keeps drawing against "Hard" concludes the game is broken.
    const xoHint = document.getElementById('xo-hint');
    function showHint(){ if (xoHint) xoHint.hidden = !(vsCpu && level === 2); }

    levelsEl.querySelectorAll('.chip').forEach(c => c.addEventListener('click', () => {
      level = +c.dataset.level;
      levelsEl.querySelectorAll('.chip').forEach(o => o.setAttribute('aria-pressed', String(o === c)));
      starter = 'X';
      showHint();
      newRound();
    }));

    document.getElementById('again').addEventListener('click', () => {
      window.AdLab && AdLab.track('game_replay', { game: 'xo' });
      newRound();
    });
    document.getElementById('reset').addEventListener('click', () => {
      wins.X = wins.O = wins.T = 0;
      updateScores();
      starter = 'X';
      newRound();
    });

    [nameX, nameO].forEach(n => n.addEventListener('input', () => { if (!over) paintTurn(); }));

    newRound();
  })();

  /* ================= Game 2: Connect Four ================= */
  (() => {
    const COLS = 7, ROWS = 6, ORDER = [3,2,4,1,5,0,6];
    const idx = (r, c) => r * COLS + c;

    // every winning quadruple on the board
    const WINDOWS = [];
    for (let r = 0; r < ROWS; r++){
      for (let c = 0; c < COLS; c++){
        if (c <= COLS - 4) WINDOWS.push([0,1,2,3].map(k => idx(r, c + k)));
        if (r <= ROWS - 4) WINDOWS.push([0,1,2,3].map(k => idx(r + k, c)));
        if (c <= COLS - 4 && r <= ROWS - 4) WINDOWS.push([0,1,2,3].map(k => idx(r + k, c + k)));
        if (c >= 3 && r <= ROWS - 4) WINDOWS.push([0,1,2,3].map(k => idx(r + k, c - k)));
      }
    }

    const gridEl = document.getElementById('c4-grid');
    if (!gridEl) return;                        // not the Connect Four page
    const colsEl = document.getElementById('c4-cols');
    const statusEl = document.getElementById('c4-status');
    const statusText = document.getElementById('c4-status-text');
    const pipEl = document.getElementById('c4-pip');
    const nameR = document.getElementById('name-r');
    const nameY = document.getElementById('name-y');
    const scR = document.getElementById('sc-r');
    const scY = document.getElementById('sc-y');
    const levelsEl = document.getElementById('c4-levels');

    let board = Array(COLS * ROWS).fill('');
    let turn = 'R';
    let over = false;
    let starter = 'R';
    let vsCpu = false;
    let level = 0;
    let busy = false;
    const wins = {R:0, Y:0, T:0};

    const nameOf = p => (p === 'R' ? nameR.value.trim() || 'Red' : nameY.value.trim() || 'Yellow');
    const colorName = p => (p === 'R' ? 'Red' : 'Yellow');

    /* ---------- Board construction ---------- */
    for (let i = 0; i < COLS * ROWS; i++){
      const h = document.createElement('div');
      h.className = 'hole';
      h.setAttribute('role', 'gridcell');
      h.setAttribute('aria-label', 'Column ' + (i % COLS + 1) + ' row ' + (Math.floor(i / COLS) + 1) + ', empty');
      gridEl.appendChild(h);
    }
    const holes = [...gridEl.children];

    const colBtns = [];
    for (let c = 0; c < COLS; c++){
      const b = document.createElement('button');
      b.className = 'col';
      b.type = 'button';
      b.dataset.c = c;
      b.setAttribute('aria-label', 'Drop a disc in column ' + (c + 1));
      b.addEventListener('click', () => drop(c));
      b.addEventListener('pointerenter', () => preview(c));
      b.addEventListener('focus', () => preview(c));
      b.addEventListener('pointerleave', clearPreview);
      b.addEventListener('blur', clearPreview);
      b.addEventListener('keydown', e => {
        const step = e.key === 'ArrowLeft' ? 1 : e.key === 'ArrowRight' ? -1 : 0;
        if (!step) return;
        e.preventDefault();
        const n = c + step;
        if (n >= 0 && n < COLS) colBtns[n].focus();
      });
      colsEl.appendChild(b);
      colBtns.push(b);
    }

    /* ---------- Logic ---------- */
    function landing(b, c){
      for (let r = ROWS - 1; r >= 0; r--) if (!b[idx(r, c)]) return idx(r, c);
      return -1;
    }

    function winnerOf(b){
      for (const w of WINDOWS){
        const a = b[w[0]];
        if (a && a === b[w[1]] && a === b[w[2]] && a === b[w[3]]) return {p:a, line:w};
      }
      return b.every(Boolean) ? {p:'T', line:null} : null;
    }

    function preview(c){
      if (over || busy) return;
      clearPreview();
      const i = landing(board, c);
      if (i < 0) return;
      holes[i].dataset.ghost = turn;
      holes[i].classList.add('preview');
    }
    function clearPreview(){ holes.forEach(h => h.classList.remove('preview')); }

    function drop(c){
      if (over || busy) return;
      const i = landing(board, c);
      if (i < 0) return;
      place(i, turn);
      beep(turn === 'R' ? 300 : 250, .14, 'square', .13);
      setTimeout(() => beep(turn === 'R' ? 150 : 130, .09, 'sine', .16), 300);
      const res = winnerOf(board);
      if (res) return finish(res);
      turn = turn === 'R' ? 'Y' : 'R';
      paintTurn();
      if (vsCpu && turn === 'Y') cpuMove();
    }

    function place(i, p){
      board[i] = p;
      const hole = holes[i];
      hole.classList.remove('preview');
      const d = document.createElement('span');
      d.className = 'disc ' + (p === 'R' ? 'r' : 'y');
      d.style.setProperty('--from', -(hole.offsetTop + hole.offsetHeight + 10) + 'px');
      hole.appendChild(d);
      hole.setAttribute('aria-label', 'Column ' + (i % COLS + 1) + ' row ' + (Math.floor(i / COLS) + 1) + ', ' + colorName(p));
    }

    function finish(res){
      over = true;
      busy = false;
      clearPreview();
      statusEl.classList.add('over');
      colBtns.forEach(b => b.disabled = true);
      if (res.p === 'T'){
        wins.T++;
        statusEl.dataset.turn = '';
        pipEl.innerHTML = '';
        window.AdLab && AdLab.track('game_end', { game: 'connect-four', outcome: 'draw', mode: vsCpu ? 'cpu' : 'two', level: level });
        tell("Board full - it's a draw \u{1F91D}");
        tune([330, 294], .14);
      } else {
        wins[res.p]++;
        statusEl.dataset.turn = res.p;
        paintPip(res.p);
        window.AdLab && AdLab.track('game_end', { game: 'connect-four', outcome: 'win', winner: res.p, mode: vsCpu ? 'cpu' : 'two', level: level });
        tell(nameOf(res.p) + ' connected four! \u{1F389}');
        setTimeout(() => res.line.forEach(i => holes[i].firstChild && holes[i].firstChild.classList.add('win')), 260);
        tune([523, 659, 784, 1047], .1);
        confetti(res.p === 'R' ? getVar('--red') : getVar('--yellow'));
      }
      updateScores();
      starter = starter === 'R' ? 'Y' : 'R';
    }

    function tell(msg){ statusText.textContent = msg; say(msg); }

    function paintPip(p){
      pipEl.innerHTML = '<span class="puck" style="background:' + (p === 'R' ? 'var(--red)' : 'var(--yellow)') + '"></span>';
    }

    function paintTurn(){
      statusEl.classList.remove('over');
      statusEl.dataset.turn = turn;
      paintPip(turn);
      const mine = vsCpu && turn === 'Y';
      statusText.textContent = mine ? 'Computer is thinking…' : nameOf(turn) + "'s turn";
      colBtns.forEach((b, c) => { b.disabled = !!mine || landing(board, c) < 0; });
      clearPreview();
      scR.classList.toggle('active', turn === 'R');
      scY.classList.toggle('active', turn === 'Y');
    }

    function updateScores(){
      document.getElementById('win-r').textContent = wins.R;
      document.getElementById('win-y').textContent = wins.Y;
      document.getElementById('win-d').textContent = wins.T;
    }

    /* ---------- Computer ---------- */
    const openCols = b => ORDER.filter(c => landing(b, c) >= 0);

    function immediate(b, p){
      for (const c of openCols(b)){
        const i = landing(b, c);
        b[i] = p;
        const w = winnerOf(b);
        b[i] = '';
        if (w && w.p === p) return c;
      }
      return -1;
    }

    function cpuMove(){
      busy = true;
      const delay = 420 + Math.random() * 340;
      setTimeout(() => {
        if (over){ busy = false; return; }
        const free = openCols(board);
        if (!free.length){ busy = false; return; }
        let c;
        if (level === 2){
          c = bestCol(6);
        } else if (level === 1){
          const wc = immediate(board, 'Y');
          const bc = immediate(board, 'R');
          if (wc >= 0) c = wc;
          else if (bc >= 0 && Math.random() < .8) c = bc;
          else c = Math.random() < .5 ? bestCol(2) : free[(Math.random() * free.length) | 0];
        } else {
          // Easy: mostly random, so kids can win
          const wc = immediate(board, 'Y');
          if (wc >= 0 && Math.random() < .5) c = wc;
          else if (Math.random() < .25) c = bestCol(2);
          else c = free[(Math.random() * free.length) | 0];
        }
        busy = false;
        drop(c);
      }, delay);
    }

    function score(b, me, opp){
      let s = 0;
      for (const w of WINDOWS){
        let m = 0, o = 0;
        for (const i of w){ if (b[i] === me) m++; else if (b[i] === opp) o++; }
        if (m && o) continue;
        if (m === 3) s += 60; else if (m === 2) s += 8; else if (m === 1) s += 1;
        if (o === 3) s -= 80; else if (o === 2) s -= 9; else if (o === 1) s -= 1;
      }
      for (let r = 0; r < ROWS; r++){
        const i = idx(r, 3);
        if (b[i] === me) s += 6; else if (b[i] === opp) s -= 6;
      }
      return s;
    }

    function minimax(b, depth, maxing, me, opp, alpha, beta){
      const w = winnerOf(b);
      if (w && w.p === me) return 100000 + depth;
      if (w && w.p === opp) return -100000 - depth;
      if (w || depth === 0) return score(b, me, opp);
      if (maxing){
        let v = -Infinity;
        for (const c of ORDER){
          const i = landing(b, c);
          if (i < 0) continue;
          b[i] = me;
          v = Math.max(v, minimax(b, depth - 1, false, me, opp, alpha, beta));
          b[i] = '';
          alpha = Math.max(alpha, v);
          if (beta <= alpha) break;
        }
        return v;
      }
      let v = Infinity;
      for (const c of ORDER){
        const i = landing(b, c);
        if (i < 0) continue;
        b[i] = opp;
        v = Math.min(v, minimax(b, depth - 1, true, me, opp, alpha, beta));
        b[i] = '';
        beta = Math.min(beta, v);
        if (beta <= alpha) break;
      }
      return v;
    }

    function bestCol(depth){
      let bestScore = -Infinity, move = -1;
      for (const c of ORDER){
        const i = landing(board, c);
        if (i < 0) continue;
        board[i] = 'Y';
        const s = minimax(board, depth - 1, false, 'Y', 'R', -Infinity, Infinity);
        board[i] = '';
        if (s > bestScore){ bestScore = s; move = c; }
      }
      return move;
    }

    /* ---------- New round ---------- */
    function newRound(){
      board = Array(COLS * ROWS).fill('');
      over = false; busy = false;
      holes.forEach((h, i) => {
        h.innerHTML = '';
        h.classList.remove('preview');
        h.setAttribute('aria-label', 'Column ' + (i % COLS + 1) + ' row ' + (Math.floor(i / COLS) + 1) + ', empty');
      });
      colBtns.forEach(b => b.disabled = false);
      turn = starter;
      paintTurn();
      if (vsCpu && turn === 'Y') cpuMove();
    }

    /* ---------- Controls ---------- */
    const modeTwo = document.getElementById('c4-mode-two');
    const modeCpu = document.getElementById('c4-mode-cpu');
    function setMode(cpu){
      vsCpu = cpu;
      modeTwo.setAttribute('aria-pressed', String(!cpu));
      modeCpu.setAttribute('aria-pressed', String(cpu));
      levelsEl.hidden = !cpu;
      nameY.disabled = cpu;
      nameY.value = cpu ? 'Computer' : 'Yellow';
      starter = 'R';
      newRound();
    }
    modeTwo.addEventListener('click', () => setMode(false));
    modeCpu.addEventListener('click', () => setMode(true));

    levelsEl.querySelectorAll('.chip').forEach(c => c.addEventListener('click', () => {
      level = +c.dataset.level;
      levelsEl.querySelectorAll('.chip').forEach(o => o.setAttribute('aria-pressed', String(o === c)));
      starter = 'R';
      newRound();
    }));

    document.getElementById('c4-again').addEventListener('click', () => {
      window.AdLab && AdLab.track('game_replay', { game: 'connect-four' });
      newRound();
    });
    document.getElementById('c4-reset').addEventListener('click', () => {
      wins.R = wins.Y = wins.T = 0;
      updateScores();
      starter = 'R';
      newRound();
    });

    [nameR, nameY].forEach(n => n.addEventListener('input', () => { if (!over) paintTurn(); }));

    newRound();
  })();
})();