/* Memory (Concentration).
 *
 * The simplest game on the site and the one most sensitive to feel. Two
 * decisions carry it:
 *
 *   1. A mismatched pair stays face-up for a beat before flipping back. Too
 *      fast and the player never sees what they revealed, which makes the game
 *      unwinnable by memory and therefore pointless. 900ms is long enough to
 *      read and short enough not to feel like a punishment.
 *
 *   2. Input is LOCKED during that beat. Without the lock, a fast tapper flips
 *      a third card mid-animation and the board desynchronises -- which reads
 *      as a bug rather than as their own mistake.
 */
(function () {
  var boardEl = document.getElementById('memory');
  if (!boardEl) return;

  var GR = window.GameRoom || {};
  var beep = GR.beep || function () {};
  var say = GR.say || function () {};

  var statusEl = document.getElementById('mm-status');
  var statusText = document.getElementById('mm-status-text');
  var movesEl = document.getElementById('mm-moves');
  var bestEl = document.getElementById('mm-best');
  var againBtn = document.getElementById('mm-again');
  var levelsEl = document.getElementById('mm-levels');

  // Inline SVG rather than glyphs OR emoji.
  //
  // Emoji render differently on every platform, and two cards that look
  // different on the player's phone are not a pair. Geometric glyphs solved
  // that and were dull -- a circle and a square are not worth remembering,
  // which is a problem in a game about remembering.
  //
  // These are drawn, so they are identical everywhere AND have some character.
  // Each is one bold silhouette: at card size, detail becomes mud.
  function sh(d, fill) {
    return '<svg viewBox="0 0 100 100" aria-hidden="true">' +
           '<path d="' + d + '" fill="' + fill + '"/></svg>';
  }
  function circ(inner, fill) {
    return '<svg viewBox="0 0 100 100" aria-hidden="true">' + inner.replace(/@/g, fill) + '</svg>';
  }

  var FACES = [
    circ('<circle cx="50" cy="55" r="30" fill="@"/><path d="M50 25 q6 -14 20 -16 q-4 14 -18 18Z" fill="#2BC4AE"/>', '#E8434F'),   // apple
    sh('M50 18 L61 42 L88 45 L68 63 L74 89 L50 76 L26 89 L32 63 L12 45 L39 42Z', '#FFB627'),                                        // star
    circ('<circle cx="50" cy="50" r="30" fill="@"/><circle cx="50" cy="50" r="13" fill="#EEF0FF"/>', '#4A4C9B'),                     // ring
    sh('M50 84 C22 64 14 46 24 33 C33 22 46 26 50 36 C54 26 67 22 76 33 C86 46 78 64 50 84Z', '#F2622E'),                            // heart
    sh('M50 12 L84 50 L50 88 L16 50Z', '#2BC4AE'),                                                                                   // diamond
    circ('<rect x="20" y="20" width="60" height="60" rx="16" fill="@"/><rect x="38" y="38" width="24" height="24" rx="7" fill="#EEF0FF"/>', '#7A5CD6'),
    sh('M20 60 q30 -46 60 0 q-30 30 -60 0Z', '#3E9BD6'),                                                                             // wave
    circ('<circle cx="50" cy="50" r="30" fill="@"/><path d="M36 50 L46 62 L66 38" stroke="#EEF0FF" stroke-width="10" fill="none" stroke-linecap="round" stroke-linejoin="round"/>', '#22B0A0'),
    sh('M50 14 L74 34 L66 84 L34 84 L26 34Z', '#E8759F'),                                                                            // gem
    circ('<path d="M50 16 L60 44 L88 44 L66 61 L74 88 L50 71 L26 88 L34 61 L12 44 L40 44Z" fill="@"/>', '#5AC8FA'),
    sh('M28 22 h44 a8 8 0 0 1 8 8 v40 a8 8 0 0 1 -8 8 h-14 l-14 14 v-14 h-16 a8 8 0 0 1 -8 -8 v-40 a8 8 0 0 1 8 -8Z', '#FF8A5B'),
    circ('<rect x="18" y="30" width="64" height="44" rx="10" fill="@"/><circle cx="36" cy="52" r="7" fill="#EEF0FF"/><circle cx="64" cy="52" r="7" fill="#EEF0FF"/>', '#4A4C9B'),
    sh('M50 16 C64 30 84 38 84 56 A34 34 0 0 1 16 56 C16 38 36 30 50 16Z', '#F5C518'),                                               // flame
    circ('<circle cx="50" cy="50" r="32" fill="@"/><path d="M50 26 v24 l16 10" stroke="#EEF0FF" stroke-width="9" fill="none" stroke-linecap="round"/>', '#6E71C9'),
    sh('M22 74 L50 20 L78 74Z', '#2BC4AE'),                                                                                          // mountain
  ];

  var size = 12, cards, flipped = [], matched = 0, moves = 0, locked = false, started = false;

  function bestKey() { return 'memory_best_' + size; }

  function readBest() {
    try { return parseInt(localStorage.getItem(bestKey()) || '0', 10) || 0; }
    catch (e) { return 0; }
  }

  function reset() {
    var pairs = size / 2;
    var deck = [];
    for (var i = 0; i < pairs; i++) {
      deck.push({ face: FACES[i % FACES.length], id: i });
      deck.push({ face: FACES[i % FACES.length], id: i });
    }
    for (var k = deck.length - 1; k > 0; k--) {
      var j = (Math.random() * (k + 1)) | 0;
      var t = deck[k]; deck[k] = deck[j]; deck[j] = t;
    }
    cards = deck;
    flipped = []; matched = 0; moves = 0; locked = false; started = false;

    // Columns chosen so the board stays close to SQUARE at every size. Three
    // columns of 12 cards is four rows, which with card-shaped tiles made a
    // board almost twice as tall as it was wide -- and a tall board is the one
    // shape that cannot fit above the fold on a phone.
    //   12 -> 4x3, 20 -> 5x4, 30 -> 6x5
    var cols = size <= 12 ? 4 : (size <= 20 ? 5 : 6);
    boardEl.style.setProperty('--cols', cols);
    boardEl.innerHTML = '';
    cards.forEach(function (c, i) {
      var b = document.createElement('button');
      b.className = 'mm-card';
      b.dataset.i = i;
      b.setAttribute('aria-label', 'Card ' + (i + 1));
      b.innerHTML = '<span class="mm-inner"><span class="mm-back"></span>' +
                    '<span class="mm-face">' + c.face + '</span></span>';
      boardEl.appendChild(b);
    });

    statusEl.classList.remove('over');
    statusText.textContent = 'Tap two cards to find a pair';
    render();
  }

  function render() {
    movesEl.textContent = moves;
    var b = readBest();
    bestEl.textContent = b ? b : '—';
  }

  boardEl.addEventListener('click', function (e) {
    var el = e.target.closest('.mm-card');
    if (!el || locked) return;
    var i = +el.dataset.i;
    if (el.classList.contains('up') || el.classList.contains('done')) return;

    if (!started) {
      started = true;
      window.AdLab && AdLab.track('game_start', { game: 'memory', pairs: size / 2 });
    }

    el.classList.add('up');
    flipped.push(i);
    beep(560, 0.05, 'square', 0.09);

    if (flipped.length < 2) return;

    moves++;
    render();
    var a = flipped[0], b = flipped[1];

    if (cards[a].id === cards[b].id) {
      flipped = [];
      matched++;
      boardEl.children[a].classList.add('done');
      boardEl.children[b].classList.add('done');
      beep(880, 0.09, 'square', 0.12);
      if (matched === size / 2) return win();
      return;
    }

    // Locked while the mismatch is visible. Without this a fast tapper flips a
    // third card mid-flight and the board desynchronises.
    locked = true;
    say('No match');
    setTimeout(function () {
      boardEl.children[a].classList.remove('up');
      boardEl.children[b].classList.remove('up');
      flipped = [];
      locked = false;
    }, 900);
  });

  function win() {
    var best = readBest();
    var isBest = best === 0 || moves < best;
    if (isBest) {
      try { localStorage.setItem(bestKey(), String(moves)); } catch (e) {}
    }
    statusEl.classList.add('over');
    statusText.textContent = isBest
      ? 'Cleared in ' + moves + ' moves — a new best!'
      : 'Cleared in ' + moves + ' moves';
    render();
    (GR.confetti || function () {})();
    (GR.tune || function () {})('win');
    window.AdLab && AdLab.track('game_end', {
      game: 'memory', outcome: 'win', pairs: size / 2, moves: moves, score: moves
    });
  }

  if (levelsEl) {
    [].forEach.call(levelsEl.querySelectorAll('.chip'), function (b) {
      b.addEventListener('click', function () {
        size = +b.dataset.level;
        [].forEach.call(levelsEl.querySelectorAll('.chip'), function (o) {
          o.setAttribute('aria-pressed', String(o === b));
        });
        reset();
      });
    });
  }

  if (againBtn) {
    againBtn.addEventListener('click', function () {
      window.AdLab && AdLab.track('game_replay', { game: 'memory', pairs: size / 2 });
      reset();
    });
  }

  reset();
})();
