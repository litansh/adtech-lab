package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var key = []byte("test-key-not-a-real-secret")

func claims() TokenClaims {
	return TokenClaims{RequestID: "req1", LineItemID: "li1", CreativeID: "cr300",
		Expires: now().Add(30 * time.Minute)}
}

func TestTokenRoundTrip(t *testing.T) {
	c, err := VerifyToken(key, MintToken(key, claims()), now())
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if c.RequestID != "req1" || c.LineItemID != "li1" || c.CreativeID != "cr300" {
		t.Errorf("claims round-tripped wrong: %+v", c)
	}
}

// The point of the whole mechanism: a hand-crafted URL cannot mint an event.
func TestForgedTokensAreRejected(t *testing.T) {
	valid := MintToken(key, claims())
	cases := map[string]string{
		"empty":                   "",
		"garbage":                 "not-a-token",
		"no signature":            strings.SplitN(valid, ".", 2)[0],
		"tampered signature":      strings.SplitN(valid, ".", 2)[0] + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"signed with another key": MintToken([]byte("attacker-key"), claims()),
	}
	for name, tok := range cases {
		if _, err := VerifyToken(key, tok, now()); err == nil {
			t.Errorf("%s: forged token was ACCEPTED", name)
		}
	}
	// Payload tampering must invalidate the signature.
	swapped := MintToken(key, TokenClaims{RequestID: "req1", LineItemID: "li-expensive",
		CreativeID: "cr300", Expires: now().Add(time.Hour)})
	sig := strings.SplitN(valid, ".", 2)[1]
	if _, err := VerifyToken(key, strings.SplitN(swapped, ".", 2)[0]+"."+sig, now()); err == nil {
		t.Error("payload swapped onto a valid signature was ACCEPTED")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	tok := MintToken(key, TokenClaims{RequestID: "r", LineItemID: "l", CreativeID: "c",
		Expires: now().Add(-time.Second)})
	if _, err := VerifyToken(key, tok, now()); err == nil {
		t.Error("expired token was ACCEPTED")
	}
}

// --------------------------------------------------------------------------
// Endpoint-level integrity
// --------------------------------------------------------------------------

func testServer() *Server {
	cp := base()
	cp.LineItems = []LineItem{{ID: "li1", CampaignID: "c1", Status: "ACTIVE", Priority: 5,
		PricingModel: "CPM", Rate: 4.00, CreativeIDs: []string{"cr300"}}}
	return &Server{cp: func() *ControlPlane { return cp }, budget: NewMemoryBudget(), sink: &StdoutSink{}, key: key, lab: false}
}

func TestImpressionRequiresValidToken(t *testing.T) {
	s := testServer()
	for _, url := range []string{"/event/impression", "/event/impression?token=forged"} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: got %d, want 403", url, w.Code)
		}
	}
	tok := MintToken(key, TokenClaims{RequestID: "r", LineItemID: "li1", CreativeID: "cr300",
		Expires: time.Now().Add(time.Hour)})
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/event/impression?token="+tok, nil))
	if w.Code != http.StatusOK {
		t.Errorf("valid token: got %d, want 200", w.Code)
	}
	// A CPM impression must charge the serving budget.
	if got := s.budget.SpendToday("li1"); got != 0.004 {
		t.Errorf("spend after one $4.00 CPM impression = %v, want 0.004", got)
	}
}

// The click endpoint must not be an open redirect: the destination comes from
// the creative record, never from the request.
func TestClickIsNotAnOpenRedirect(t *testing.T) {
	s := testServer()
	tok := MintToken(key, TokenClaims{RequestID: "r", LineItemID: "li1", CreativeID: "cr300",
		Expires: time.Now().Add(time.Hour)})

	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/event/click?token="+tok+"&url=https://evil.example&redirect=https://evil.example", nil))

	if w.Code != http.StatusFound {
		t.Fatalf("got %d, want 302", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "https://example.com" {
		t.Errorf("redirected to %q -- attacker-supplied destination was honoured", loc)
	}
	if strings.Contains(loc, "evil.example") {
		t.Error("OPEN REDIRECT: attacker URL reached the Location header")
	}
}

// The Decision Trace must never reach a production response.
func TestTraceIsLabOnly(t *testing.T) {
	body := `{"placement_id":"game_sidebar","game":"xo","device_type":"desktop","session_id":"s"}`

	prod := testServer() // lab:false
	w := httptest.NewRecorder()
	prod.routes().ServeHTTP(w, adRequest(body))
	if strings.Contains(w.Body.String(), `"trace"`) {
		t.Error("production response leaked the Decision Trace")
	}
	if !strings.Contains(w.Body.String(), `"request_id"`) {
		t.Error("production response must still carry a request_id for investigation")
	}

	lab := testServer()
	lab.lab = true
	w2 := httptest.NewRecorder()
	lab.routes().ServeHTTP(w2, adRequest(body))
	if !strings.Contains(w2.Body.String(), `"trace"`) {
		t.Error("lab response should include the Decision Trace")
	}
}

// adRequest builds an ad request the way a browser actually sends one. A real
// browser always sends a User-Agent; a request without one is classified as
// non-billable traffic, so tests that omit it are testing a path no browser
// takes. See filter.go.
func adRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/ad/request", strings.NewReader(body))
	r.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "+
		"AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	return r
}
