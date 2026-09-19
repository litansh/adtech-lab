package main

import (
	"testing"
	"time"
)

func capped(n int) FrequencyCap { return FrequencyCap{Impressions: n, Scope: ScopeSession} }

func TestCapAllowsExactlyItsLimit(t *testing.T) {
	fs := NewMemoryFrequency(time.Hour)
	c := capped(3)
	for i := 0; i < 3; i++ {
		if c.Capped(fs, "s1", "li1") {
			t.Fatalf("capped after only %d impressions, limit is 3", i)
		}
		fs.Increment("s1", "li1")
	}
	if !c.Capped(fs, "s1", "li1") {
		t.Fatal("not capped after 3 impressions")
	}
}

func TestCapIsPerSessionAndPerSubject(t *testing.T) {
	fs := NewMemoryFrequency(time.Hour)
	c := capped(1)
	fs.Increment("s1", "li1")

	if !c.Capped(fs, "s1", "li1") {
		t.Error("s1/li1 should be capped")
	}
	if c.Capped(fs, "s2", "li1") {
		t.Error("a different session inherited s1's count")
	}
	if c.Capped(fs, "s1", "li2") {
		t.Error("a different line item inherited li1's count")
	}
}

func TestZeroMeansUncapped(t *testing.T) {
	fs := NewMemoryFrequency(time.Hour)
	c := FrequencyCap{}
	for i := 0; i < 50; i++ {
		fs.Increment("s1", "li1")
	}
	if c.Capped(fs, "s1", "li1") {
		t.Fatal("a line item with no cap was capped")
	}
}

// A cap that fires when it cannot measure is a cap that silently kills
// delivery. No session means no counting, so no cap applies.
func TestNoSessionMeansNoCap(t *testing.T) {
	fs := NewMemoryFrequency(time.Hour)
	c := capped(1)
	for i := 0; i < 10; i++ {
		fs.Increment("", "li1")
	}
	if c.Capped(fs, "", "li1") {
		t.Fatal("a request with no session id was capped")
	}
}

// An unimplemented scope must not silently behave like a different one.
func TestUnknownScopeServesUncapped(t *testing.T) {
	fs := NewMemoryFrequency(time.Hour)
	fs.Increment("s1", "li1")
	fs.Increment("s1", "li1")
	c := FrequencyCap{Impressions: 1, Scope: "day"} // not implemented
	if c.Capped(fs, "s1", "li1") {
		t.Fatal("an unimplemented 'day' scope silently applied session semantics")
	}
}

func TestCountsExpireWithTheSession(t *testing.T) {
	fs := NewMemoryFrequency(30 * time.Minute)
	base := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	fs.now = func() time.Time { return base }

	fs.Increment("s1", "li1")
	fs.Increment("s1", "li1")
	if got := fs.Count("s1", "li1"); got != 2 {
		t.Fatalf("count %d, want 2", got)
	}

	fs.now = func() time.Time { return base.Add(31 * time.Minute) }
	if got := fs.Count("s1", "li1"); got != 0 {
		t.Fatalf("count %d after the TTL, want 0", got)
	}
	// And a later impression starts from one, not from three.
	fs.Increment("s1", "li1")
	if got := fs.Count("s1", "li1"); got != 1 {
		t.Fatalf("count %d after expiry and one increment, want 1", got)
	}
}

// End to end through Decide: a capped line item is rejected with the SPECIFIC
// reason, because the Trace is only useful if the reason it gives is the real
// one.
func TestDecideReportsFrequencyCappedSpecifically(t *testing.T) {
	cp, err := loadControlPlane("fixtures/control-plane.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Cap every line item at one impression per session.
	for i := range cp.LineItems {
		cp.LineItems[i].FrequencyCap = capped(1)
	}
	fs := NewMemoryFrequency(time.Hour)
	b := NewMemoryBudget()
	r := AdRequest{PlacementID: cp.Placements[0].ID, Game: "xo",
		DeviceType: "mobile", Country: "IL", SessionID: "s1"}

	li, _, tr := Decide(cp, &r, &cp.Placements[0], b, fs, nil, time.Now())
	if li == nil {
		t.Fatalf("nothing served on the first request: %s", tr.DecisionRule)
	}
	fs.Increment("s1", li.ID)

	// Everything is capped at 1, so the second request in this session has no
	// eligible line item, and every candidate says why.
	for i := range cp.LineItems {
		fs.Increment("s1", cp.LineItems[i].ID)
	}
	li2, _, tr2 := Decide(cp, &r, &cp.Placements[0], b, fs, nil, time.Now())
	if li2 != nil {
		t.Fatalf("served %s despite every line item being capped", li2.ID)
	}
	found := false
	for _, c := range tr2.Candidates {
		if c.Reason == ReasonFrequencyCapped {
			found = true
			if c.FrequencyCap != 1 || c.FrequencySeen < 1 {
				t.Errorf("trace lacks the numbers: seen=%d cap=%d",
					c.FrequencySeen, c.FrequencyCap)
			}
		}
	}
	if !found {
		t.Fatalf("no candidate reported %q; reasons were %v",
			ReasonFrequencyCapped, reasonsOf(tr2))
	}

	// A different session is unaffected -- the cap is per person, not global.
	r2 := r
	r2.SessionID = "s2"
	if li3, _, _ := Decide(cp, &r2, &cp.Placements[0], b, fs, nil, time.Now()); li3 == nil {
		t.Fatal("a fresh session was blocked by another session's cap")
	}
}

func reasonsOf(tr *Trace) []string {
	var out []string
	for _, c := range tr.Candidates {
		out = append(out, c.Reason)
	}
	return out
}

// The behaviour that matters to a player: once the paying line items are
// capped, the slot still fills -- with the house ad -- rather than going blank.
func TestCappedSessionFallsThroughToHouse(t *testing.T) {
	cp, err := loadControlPlane("fixtures/control-plane.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	fs := NewMemoryFrequency(time.Hour)
	b := NewMemoryBudget()
	// acme targets IL + desktop. The first version of this test used mobile,
	// so acme was rejected for device_mismatch and the house ad served from
	// request one -- the test passed while exercising nothing.
	r := AdRequest{PlacementID: cp.Placements[0].ID, Game: "xo",
		DeviceType: "desktop", Country: "IL", SessionID: "s1"}

	var served []string
	for i := 0; i < 8; i++ {
		li, _, _ := Decide(cp, &r, &cp.Placements[0], b, fs, nil, time.Now())
		if li == nil {
			t.Fatalf("request %d served nothing at all", i)
		}
		served = append(served, li.ID)
		if li.FrequencyCap.Impressions > 0 {
			fs.Increment(r.SessionID, li.ID)
		}
	}
	// The paying line item must serve first, up to its cap, and only then
	// should the house ad take over. Both halves matter.
	if served[0] != "li-acme-cpm" {
		t.Fatalf("first request served %q, want the paying line item", served[0])
	}
	paid := 0
	for _, id := range served {
		if id == "li-acme-cpm" {
			paid++
		}
	}
	if paid != 3 {
		t.Errorf("paying line item served %d times, want exactly its cap of 3", paid)
	}
	if last := served[len(served)-1]; last != "li-house-fallback" {
		t.Fatalf("after exhausting the cap the slot served %q, want the house ad", last)
	}
	t.Logf("served in order: %v", served)
}

// The bug that shipped: FrequencyState was wired only in the local branch of
// main(), so s.freq was nil in Lambda and every cap silently did nothing. This
// asserts the interface, but the REAL lesson is that no unit test could have
// caught it -- the gap was in wiring, not in logic.
//
// What catches it is counting impressions against the live endpoint, which is
// now part of `make check-prod`.
func TestNilFrequencyStateServesUncapped(t *testing.T) {
	c := capped(1)
	if c.Capped(nil, "s1", "li1") {
		t.Fatal("a nil store must fail open, not closed")
	}
}

// Both implementations must agree, or the behaviour differs by environment --
// which is exactly the class of bug this whole file is about.
func TestMemoryAndDynamoFrequencyShareTheirContract(t *testing.T) {
	// Only the memory one can run without AWS; this asserts the shape both
	// satisfy, so a future divergence is a compile error rather than a
	// production surprise.
	var _ FrequencyState = (*MemoryFrequency)(nil)
	var _ FrequencyState = (*DynamoFrequency)(nil)

	m := NewMemoryFrequency(time.Hour)
	if m.Count("", "li") != 0 {
		t.Error("an empty session must count as zero, not panic")
	}
	m.Increment("", "li") // must not panic
}
