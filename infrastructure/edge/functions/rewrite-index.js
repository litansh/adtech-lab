// CloudFront Function (viewer request).
//
// S3 has no concept of directories: a request for /xo/ maps to the key "xo/",
// which does not exist, and default_root_object only applies at the root. So
// rewrite directory-style paths onto their index.html.
//
// No www handling. The domain is new and nothing has ever linked to
// www.xoxoxo.live, so supporting it would mean a certificate SAN, a DNS record
// and a redirect branch to prevent a broken link that cannot yet exist. If that
// changes, add the alias, the SAN and a 301 here -- the apex stays canonical.
//
// Runs at the edge in ~1ms. 2M invocations/month are free.
function handler(event) {
  var request = event.request;
  var uri = request.uri;

  if (uri.endsWith('/')) {
    request.uri = uri + 'index.html';
  } else if (!uri.includes('.')) {
    request.uri = uri + '/index.html';   // /xo -> /xo/index.html
  }
  return request;
}
