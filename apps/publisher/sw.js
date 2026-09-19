/* Offline play. The games are entirely client-side, so once the shell is cached
 * they work with no network at all -- which is the whole point of installing.
 *
 * Deliberately NOT cached: /ad/request, /event/*, /collect. An ad decision must
 * never be served from a cache -- two users would share an impression, and a
 * stale impression beacon would be a false count. Ads simply fail offline, and
 * the slot collapses, exactly as it does when the ad server is down. */
var CACHE = 'gameroom-v1';
var SHELL = ['/', '/xo', '/connect-four', '/snake', '/privacy', '/favicon.svg', '/site.webmanifest'];

self.addEventListener('install', function (e) {
  e.waitUntil(caches.open(CACHE).then(function (c) { return c.addAll(SHELL); }).then(function () {
    return self.skipWaiting();
  }));
});

self.addEventListener('activate', function (e) {
  e.waitUntil(caches.keys().then(function (keys) {
    return Promise.all(keys.filter(function (k) { return k !== CACHE; })
      .map(function (k) { return caches.delete(k); }));
  }).then(function () { return self.clients.claim(); }));
});

self.addEventListener('fetch', function (e) {
  var url = new URL(e.request.url);
  if (e.request.method !== 'GET') return;
  if (url.origin !== location.origin) return;
  if (/^\/(ad|event|collect)/.test(url.pathname)) return;   // never cache ad traffic

  // Hashed assets are immutable: cache-first is safe and instant.
  // Everything else: network-first, falling back to cache when offline.
  var immutable = /\/static\/.+\.[0-9a-f]{10}\.(js|css)$/.test(url.pathname);
  if (immutable) {
    e.respondWith(caches.match(e.request).then(function (hit) {
      return hit || fetch(e.request).then(function (res) {
        var copy = res.clone();
        caches.open(CACHE).then(function (c) { c.put(e.request, copy); });
        return res;
      });
    }));
    return;
  }
  e.respondWith(fetch(e.request).then(function (res) {
    var copy = res.clone();
    caches.open(CACHE).then(function (c) { c.put(e.request, copy); });
    return res;
  }).catch(function () { return caches.match(e.request); }));
});
