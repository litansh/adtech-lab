package main

import "testing"

func sound() SessionView {
	return SessionView{
		SessionID:  "s1",
		GameStarts: 2,
		Countries:  []string{"IL", "IL"},
		RequestTS:  []int64{1000, 4300, 9100, 15600, 22000, 31500, 44000, 51200},
		Impressions: []Impression{
			{EventID: "e1", TSMillis: 1000, Revenue: 0.004},
			{EventID: "e2", TSMillis: 9000, Revenue: 0.003, Clicked: true, ClickTS: 12400},
			{EventID: "e3", TSMillis: 22000, Revenue: 0.005},
			{EventID: "e4", TSMillis: 44000, Revenue: 0.004},
		},
	}
}

func TestOrdinarySessionIsNotRevoked(t *testing.T) {
	if r := Review(sound(), DefaultThresholds()); r != nil {
		t.Fatalf("a normal session was revoked: %v", r.Reasons)
	}
}

func TestClickBeforeRenderIsRevoked(t *testing.T) {
	s := sound()
	// 40ms after the impression: nobody saw a creative and moved a finger.
	s.Impressions[1].ClickTS = s.Impressions[1].TSMillis + 40
	r := Review(s, DefaultThresholds())
	if r == nil || !has(r.Reasons, ReasonClickBeforeRender) {
		t.Fatalf("impossible click not caught: %v", r)
	}
}

func TestSlowClickIsFine(t *testing.T) {
	s := sound()
	s.Impressions[1].ClickTS = s.Impressions[1].TSMillis + 900
	if r := Review(s, DefaultThresholds()); r != nil {
		t.Fatalf("a 900ms click was revoked: %v", r.Reasons)
	}
}

// Ads served into a session where nobody ever started a game. After the fact
// this is what a prefetch, a scraper or a headless browser looks like.
func TestImpressionsWithoutPlayAreRevoked(t *testing.T) {
	s := sound()
	s.GameStarts = 0
	r := Review(s, DefaultThresholds())
	if r == nil || !has(r.Reasons, ReasonImpressionsWithoutPlay) {
		t.Fatalf("impressions with no gameplay not caught: %v", r)
	}
}

// A session with no impressions and no play is just someone who left. There is
// nothing to revoke, and flagging it would be noise.
func TestNoImpressionsMeansNothingToRevoke(t *testing.T) {
	s := SessionView{SessionID: "s", GameStarts: 0}
	if r := Review(s, DefaultThresholds()); r != nil {
		t.Fatalf("a session with nothing billable was flagged: %v", r.Reasons)
	}
}

func TestImplausibleCTRNeedsVolume(t *testing.T) {
	th := DefaultThresholds()

	// One click on one impression is 100% CTR and means nothing.
	small := SessionView{SessionID: "s", GameStarts: 1, Countries: []string{"IL"},
		Impressions: []Impression{{EventID: "a", TSMillis: 0, Clicked: true, ClickTS: 5000}}}
	if r := Review(small, th); r != nil && has(r.Reasons, ReasonImplausibleCTR) {
		t.Error("100% CTR on a single impression was treated as a signal")
	}

	// Four impressions, four clicks, each plausibly timed, is not a person.
	big := SessionView{SessionID: "s", GameStarts: 1, Countries: []string{"IL"}}
	for i := 0; i < 4; i++ {
		ts := int64(i * 10000)
		big.Impressions = append(big.Impressions, Impression{
			EventID: "e", TSMillis: ts, Clicked: true, ClickTS: ts + 4000})
	}
	r := Review(big, th)
	if r == nil || !has(r.Reasons, ReasonImplausibleCTR) {
		t.Fatalf("100%% CTR across four impressions not caught: %v", r)
	}
}

func TestSessionAcrossCountriesIsRevoked(t *testing.T) {
	s := sound()
	s.Countries = []string{"IL", "DE", "IL"}
	r := Review(s, DefaultThresholds())
	if r == nil || !has(r.Reasons, ReasonMultipleCountries) {
		t.Fatalf("a session in two countries not caught: %v", r)
	}
}

func TestMetronomicTimingIsRevoked(t *testing.T) {
	s := sound()
	s.RequestTS = nil
	for i := 0; i < 12; i++ {
		s.RequestTS = append(s.RequestTS, int64(i)*5000) // exactly 5s apart
	}
	r := Review(s, DefaultThresholds())
	if r == nil || !has(r.Reasons, ReasonMetronomicTiming) {
		t.Fatalf("perfectly regular timing not caught: %v", r)
	}
}

func TestHumanTimingIsNotMetronomic(t *testing.T) {
	if r := Review(sound(), DefaultThresholds()); r != nil {
		t.Fatalf("irregular human timing was flagged: %v", r.Reasons)
	}
}

func TestTimingNeedsEnoughRequests(t *testing.T) {
	s := sound()
	s.RequestTS = []int64{0, 5000, 10000} // regular, but only three
	if r := Review(s, DefaultThresholds()); r != nil && has(r.Reasons, ReasonMetronomicTiming) {
		t.Error("three regular requests was treated as a signal")
	}
}

// Every impression in a revoked session is listed, with the revenue, because
// 'why was this not billed?' has to be answerable per event months later.
func TestRevocationListsEveryEventAndItsRevenue(t *testing.T) {
	s := sound()
	s.GameStarts = 0
	r := Review(s, DefaultThresholds())
	if r == nil {
		t.Fatal("expected a revocation")
	}
	if len(r.EventIDs) != len(s.Impressions) {
		t.Errorf("listed %d of %d events", len(r.EventIDs), len(s.Impressions))
	}
	var want float64
	for _, im := range s.Impressions {
		want += im.Revenue
	}
	if r.Revenue != want {
		t.Errorf("revenue %.6f, want %.6f", r.Revenue, want)
	}
}

func has(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
