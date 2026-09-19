/* Personal records table. Reads what adlab.js already recorded from game_end,
 * so it needs no per-game wiring and no server.
 *
 * Hidden entirely when there is nothing to show: an empty "Your records" table
 * on a first visit is a promise the site has not earned yet. */
(function () {
  var host = document.getElementById('records');
  var body = document.getElementById('records-body');
  if (!host || !body || !window.AdLab || !AdLab.records) return;

  var NAMES = {
    'xo': 'Tic-Tac-Toe',
    'connect-four': 'Connect Four',
    'snake': 'Snake',
    '2048': '2048'
  };
  // Only these two produce a score worth calling a "best"; for the others the
  // column would always read 0, which looks like a bug rather than a blank.
  var SCORED = { 'snake': true, '2048': true };
  var ORDER = ['xo', 'connect-four', 'snake', '2048'];

  var all = AdLab.records();
  var rows = 0;

  ORDER.forEach(function (key) {
    var r = all[key];
    if (!r || !r.played) return;
    var tr = document.createElement('tr');
    tr.innerHTML =
      '<td>' + NAMES[key] + '</td>' +
      '<td>' + r.played + '</td>' +
      '<td>' + (r.won || 0) + '</td>' +
      '<td>' + (SCORED[key] ? (r.best || 0) : '&mdash;') + '</td>';
    body.appendChild(tr);
    rows++;
  });

  if (rows) host.hidden = false;
})();
