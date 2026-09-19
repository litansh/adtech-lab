/* The daily hub — what you did today, memorialised on the card.
 *
 * Proposed by the Product agent (#56) as the direction rather than the next
 * tweak, and reached independently from the other side by the Horizon agent,
 * whose 260-item survey found "daily" the loudest pattern in the whole
 * landscape — ahead of every individual game mechanic.
 *
 * The idea is Puzzmo's and worth stating plainly: a page of puzzle widgets that
 * shows your progress when you navigate back, the way a newspaper's puzzle
 * section is memorialised in pencil. The games were already good; what was
 * missing was anything that made six of them feel like one visit.
 *
 * Nothing here is a score. It is a record of a day, and it resets at midnight
 * UTC with the daily puzzle, so the page is about today rather than about how
 * much you have ever played.
 */
(function () {
  'use strict';

  var KEY = 'adlab_today_v1';

  function todayUTC() {
    return new Date().toISOString().slice(0, 10);
  }

  /* One day's play, and only one. Reading a stale day returns nothing rather
   * than yesterday's results dressed up as today's: a hub that shows a
   * three-day-old win is worse than a hub that shows nothing, because it
   * removes the reason to come back. */
  function read() {
    try {
      var raw = JSON.parse(localStorage.getItem(KEY));
      if (!raw || raw.date !== todayUTC()) return {};
      return raw.games || {};
    } catch (e) {
      return {};
    }
  }

  function write(game, entry) {
    try {
      var raw = JSON.parse(localStorage.getItem(KEY));
      if (!raw || raw.date !== todayUTC()) raw = { date: todayUTC(), games: {} };
      raw.games[game] = entry;
      localStorage.setItem(KEY, JSON.stringify(raw));
    } catch (e) { /* storage off: the hub degrades to plain cards */ }
  }

  /* How a day's result reads, per game. Deliberately short — a card has room
   * for about four words, and "Solved in 4:12" is worth more than a sentence. */
  function describe(game, e) {
    if (!e) return '';
    switch (game) {
      case 'daily':
        return e.outcome === 'win'
          ? 'Solved' + (e.seconds ? ' in ' + mmss(e.seconds) : '')
          : 'Started';
      case '2048':
      case 'snake':
        return isFinite(e.score) ? 'Scored ' + e.score : 'Played';
      case 'memory':
        return isFinite(e.moves) ? 'Done in ' + e.moves + ' moves' : 'Played';
      case 'sudoku':
        return e.outcome === 'win' ? 'Solved' : 'Played';
      default:
        /* Adversarial games have an outcome, not a number. */
        if (e.outcome === 'win') return 'Won';
        if (e.outcome === 'lose') return 'Lost';
        return 'Played';
    }
  }

  function mmss(total) {
    var m = Math.floor(total / 60), s = total % 60;
    return m + ':' + (s < 10 ? '0' : '') + s;
  }

  function paint() {
    var played = read();
    var any = false;

    var cards = document.querySelectorAll('[data-game]');
    for (var i = 0; i < cards.length; i++) {
      var card = cards[i];
      var game = card.getAttribute('data-game');
      var entry = played[game];
      var slot = card.querySelector('.gc-today, .db-today');
      if (!slot) continue;

      var text = describe(game, entry);
      if (text) {
        slot.textContent = text;
        slot.hidden = false;
        card.setAttribute('data-played', 'today');
        any = true;
      } else {
        slot.hidden = true;
        card.removeAttribute('data-played');
      }
    }

    /* A count, but only once there is something to count. "0 of 7 today" on a
     * first visit is a scoreboard telling a new player they are behind. */
    var summary = document.getElementById('today-summary');
    if (summary) {
      var n = Object.keys(played).length;
      if (any && n > 0) {
        summary.textContent = n === 1
          ? 'One game played today.'
          : n + ' games played today.';
        summary.hidden = false;
      } else {
        summary.hidden = true;
      }
    }
  }

  /* Recording happens wherever a game ends, which is any page, so it hooks the
   * same event the rest of the site already emits rather than asking six games
   * to call something new. */
  function record(ev) {
    if (!ev || !ev.game) return;
    write(ev.game, {
      outcome: ev.outcome,
      score: Number(ev.score),
      moves: Number(ev.moves),
      seconds: Number(ev.seconds)
    });
  }

  if (window.AdLab && typeof window.AdLab.track === 'function') {
    var original = window.AdLab.track;
    window.AdLab.track = function (name, props) {
      if (name === 'game_end') { try { record(props); } catch (e) {} }
      return original.apply(window.AdLab, arguments);
    };
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', paint);
  } else {
    paint();
  }

  window.Today = { describe: describe, paint: paint, read: read, write: write };
})();
