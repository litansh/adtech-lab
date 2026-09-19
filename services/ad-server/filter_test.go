package main

import (
	"strings"
	"testing"
	"time"
)

const chromeUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"

func human() RequestSignals {
	return RequestSignals{
		UserAgent: chromeUA, IP: "82.166.4.10", Width: 300, Height: 250,
		DeviceType: "mobile", SessionRequests: 3, SessionAge: 90 * time.Second,
	}
}

func TestOrdinaryTrafficIsBillable(t *testing.T) {
	a := ClassifyTraffic(human())
	if a.Class != ClassHuman || !a.Billable {
		t.Fatalf("got %s billable=%v reasons=%v, want human/billable", a.Class, a.Billable, a.Reasons)
	}
}

// The central rule of this file: declaring yourself must never be punished.
func TestDeclaredAgentIsNotTreatedAsFraud(t *testing.T) {
	s := human()
	s.AgentDeclaration = "shopping-assistant/1.2"
	a := ClassifyTraffic(s)
	if a.Class != ClassDeclaredAgent {
		t.Fatalf("declared agent classified as %s, want declared_agent", a.Class)
	}
	if a.Billable {
		t.Error("declared agent must not be billable -- nothing was displayed to a person")
	}
	if a.AgentName != "shopping-assistant/1.2" {
		t.Errorf("agent name not recorded: %q", a.AgentName)
	}
	for _, r := range a.Reasons {
		if r != ReasonDeclaredAgentHeader {
			t.Errorf("declared agent picked up a suspicion reason: %s", r)
		}
	}
}

// An agent that declares itself while ALSO tripping every fraud heuristic must
// still be classed as declared. Otherwise honesty is strictly worse than
// silence, and no agent declares itself twice.
func TestDeclarationBeatsSuspicion(t *testing.T) {
	s := RequestSignals{
		AgentDeclaration: "research-agent/2",
		UserAgent:        "HeadlessChrome/120.0",
		IP:               "35.190.1.1", // datacentre
		Width:            99999, Height: 99999,
	}
	a := ClassifyTraffic(s)
	if a.Class != ClassDeclaredAgent {
		t.Fatalf("got %s, want declared_agent even with suspicious signals", a.Class)
	}
}

func TestKnownCrawlersAreDeclaredNotFraud(t *testing.T) {
	for _, ua := range []string{
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.1; +https://openai.com/gptbot)",
		"Mozilla/5.0 (compatible; ClaudeBot/1.0; +claudebot@anthropic.com)",
		"Mozilla/5.0 (compatible; PerplexityBot/1.0)",
	} {
		s := human()
		s.UserAgent = ua
		a := ClassifyTraffic(s)
		if a.Class != ClassDeclaredAgent {
			t.Errorf("%.40s classified %s, want declared_agent", ua, a.Class)
		}
		if a.Billable {
			t.Errorf("%.40s was billable", ua)
		}
	}
}

func TestUndeclaredAutomationIsSuspected(t *testing.T) {
	cases := map[string]func(*RequestSignals){
		ReasonHeadlessUA:         func(s *RequestSignals) { s.UserAgent = "Mozilla/5.0 HeadlessChrome/120.0.0.0" },
		ReasonNoUserAgent:        func(s *RequestSignals) { s.UserAgent = "" },
		ReasonDatacenterIP:       func(s *RequestSignals) { s.IP = "35.190.12.34" },
		ReasonImpossibleViewport: func(s *RequestSignals) { s.Width, s.Height = 99999, 99999 },
		ReasonPrerender:          func(s *RequestSignals) { s.SecPurpose = "prefetch" },
		ReasonUAMismatch: func(s *RequestSignals) {
			s.DeviceType = "mobile"
			s.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120"
		},
		ReasonSessionRate: func(s *RequestSignals) {
			s.SessionRequests = 400
			s.SessionAge = 30 * time.Second
		},
	}
	for wantReason, mutate := range cases {
		s := human()
		mutate(&s)
		a := ClassifyTraffic(s)
		if a.Class != ClassSuspectedIVT {
			t.Errorf("%s: got %s, want suspected_ivt", wantReason, a.Class)
			continue
		}
		if a.Billable {
			t.Errorf("%s: suspected IVT was billable", wantReason)
		}
		if !contains(a.Reasons, wantReason) {
			t.Errorf("%s: reasons were %v", wantReason, a.Reasons)
		}
	}
}

// A page loading normally produces several quick requests. That must not look
// like an attack, or every real player is misclassified on arrival.
func TestBurstAtPageLoadIsNotIVT(t *testing.T) {
	s := human()
	s.SessionRequests = 4
	s.SessionAge = 900 * time.Millisecond
	if a := ClassifyTraffic(s); a.Class != ClassHuman {
		t.Fatalf("page-load burst classified %s (%v)", a.Class, a.Reasons)
	}
}

func TestClassificationIsPureAndRepeatable(t *testing.T) {
	s := human()
	s.UserAgent = "HeadlessChrome/120"
	s.IP = "35.190.1.1"
	first := ClassifyTraffic(s)
	for i := 0; i < 100; i++ {
		got := ClassifyTraffic(s)
		if got.Class != first.Class || len(got.Reasons) != len(first.Reasons) {
			t.Fatal("classification is not deterministic")
		}
	}
	if len(first.Reasons) < 2 {
		t.Errorf("want every trigger recorded for audit, got %v", first.Reasons)
	}
}

func TestDatacenterDetectionDoesNotFlagResidential(t *testing.T) {
	for _, ip := range []string{"82.166.4.10", "77.127.1.5", "2.55.1.1", "not-an-ip", ""} {
		if isDatacenterIP(ip) {
			t.Errorf("%q flagged as datacentre", ip)
		}
	}
	for _, ip := range []string{"35.190.1.1", "52.1.2.3", "159.65.9.9"} {
		if !isDatacenterIP(ip) {
			t.Errorf("%q not flagged as datacentre", ip)
		}
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if strings.EqualFold(x, want) {
			return true
		}
	}
	return false
}

// Absent dimensions are normal -- the placement defines the slot size.
func TestMissingDimensionsAreNotSuspicious(t *testing.T) {
	s := human()
	s.Width, s.Height = 0, 0
	if a := ClassifyTraffic(s); a.Class != ClassHuman {
		t.Fatalf("request without w/h classified %s (%v)", a.Class, a.Reasons)
	}
}
