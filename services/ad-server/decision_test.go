package main

import (
	"testing"
	"time"
)

func now() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) }

func base() *ControlPlane {
	return &ControlPlane{
		Placements: []Placement{{ID: "game_sidebar", SiteID: "s1", Width: 300, Height: 250}},
		Campaigns: []Campaign{
			{ID: "c1", Status: "ACTIVE", FlightStart: now().Add(-24 * time.Hour), FlightEnd: now().Add(24 * time.Hour)},
			{ID: "cpaused", Status: "PAUSED", FlightStart: now().Add(-24 * time.Hour), FlightEnd: now().Add(24 * time.Hour)},
		},
		Creatives: []Creative{
			{ID: "cr300", Width: 300, Height: 250, HTML: "<b>ad</b>", ClickURL: "https://example.com"},
			{ID: "cr728", Width: 728, Height: 90, HTML: "<b>wide</b>", ClickURL: "https://example.com"},
		},
	}
}

func req() *AdRequest {
	return &AdRequest{PlacementID: "game_sidebar", Game: "xo", DeviceType: "desktop", Country: "IL"}
}

func decide(cp *ControlPlane, b BudgetState) (*LineItem, *Trace) {
	li, _, tr := Decide(cp, req(), &cp.Placements[0], b, nil, nil, now())
	return li, tr
}

// eCPM normalisation is the comparison that makes mixed demand rankable.
func TestExpectedECPM(t *testing.T) {
	cases := []struct {
		name string
		li   LineItem
		want float64
	}{
		{"CPM is already per-1000", LineItem{PricingModel: "CPM", Rate: 4.00}, 4.00},
		{"CPC $0.40 at 0.8% CTR", LineItem{PricingModel: "CPC", Rate: 0.40, ExpectedCTR: 0.008}, 3.20},
		{"CPA $8 at 0.8% CTR, 5% CVR", LineItem{PricingModel: "CPA", Rate: 8.00, ExpectedCTR: 0.008, ExpectedCVR: 0.05}, 3.20},
	}
	for _, c := range cases {
		if got := expectedECPM(&c.li); got != c.want {
			t.Errorf("%s: got %.4f want %.4f", c.name, got, c.want)
		}
	}
}

// Priority outranks price. This is the lesson "rank by eCPM" hides.
func TestPriorityBeatsHigherValue(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{
		{ID: "guaranteed", CampaignID: "c1", Status: "ACTIVE", Priority: 10,
			PricingModel: "CPM", Rate: 12.00, CreativeIDs: []string{"cr300"}},
		{ID: "remnant", CampaignID: "c1", Status: "ACTIVE", Priority: 1,
			PricingModel: "CPM", Rate: 15.00, CreativeIDs: []string{"cr300"}},
	}
	li, tr := decide(cp, NewMemoryBudget())
	if li == nil || li.ID != "guaranteed" {
		t.Fatalf("expected the $12 guaranteed line item to beat the $15 remnant, got %v", li)
	}
	if tr.DecisionRule != "highest_priority" {
		t.Errorf("decision rule = %q, want highest_priority", tr.DecisionRule)
	}
}

// At equal priority, expected value decides -- across pricing models.
func TestEqualPriorityRanksByExpectedECPM(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{
		{ID: "cpm4", CampaignID: "c1", Status: "ACTIVE", Priority: 5,
			PricingModel: "CPM", Rate: 4.00, CreativeIDs: []string{"cr300"}},
		{ID: "cpc-rich", CampaignID: "c1", Status: "ACTIVE", Priority: 5,
			PricingModel: "CPC", Rate: 0.50, ExpectedCTR: 0.012, CreativeIDs: []string{"cr300"}}, // eCPM 6.00
	}
	li, tr := decide(cp, NewMemoryBudget())
	if li == nil || li.ID != "cpc-rich" {
		t.Fatalf("expected the CPC line item (eCPM $6.00) to win, got %v", li)
	}
	if tr.DecisionRule != "highest_expected_ecpm" {
		t.Errorf("decision rule = %q, want highest_expected_ecpm", tr.DecisionRule)
	}
}

func TestEligibilityRejections(t *testing.T) {
	cases := []struct {
		name   string
		li     LineItem
		reason string
	}{
		{"paused line item", LineItem{ID: "x", CampaignID: "c1", Status: "PAUSED", CreativeIDs: []string{"cr300"}}, ReasonLineItemPaused},
		{"paused campaign", LineItem{ID: "x", CampaignID: "cpaused", Status: "ACTIVE", CreativeIDs: []string{"cr300"}}, ReasonCampaignPaused},
		{"geo mismatch", LineItem{ID: "x", CampaignID: "c1", Status: "ACTIVE", CreativeIDs: []string{"cr300"},
			Targeting: Targeting{Countries: []string{"US"}}}, ReasonGeoMismatch},
		{"device mismatch", LineItem{ID: "x", CampaignID: "c1", Status: "ACTIVE", CreativeIDs: []string{"cr300"},
			Targeting: Targeting{DeviceTypes: []string{"mobile"}}}, ReasonDeviceMismatch},
		{"game mismatch", LineItem{ID: "x", CampaignID: "c1", Status: "ACTIVE", CreativeIDs: []string{"cr300"},
			Targeting: Targeting{Games: []string{"connect-four"}}}, ReasonGameMismatch},
		{"no creative fits the slot", LineItem{ID: "x", CampaignID: "c1", Status: "ACTIVE", CreativeIDs: []string{"cr728"}}, ReasonNoCreativeThatFits},
	}
	for _, c := range cases {
		cp := base()
		cp.LineItems = []LineItem{c.li}
		li, tr := decide(cp, NewMemoryBudget())
		if li != nil {
			t.Errorf("%s: expected no winner", c.name)
			continue
		}
		if tr.Candidates[0].Reason != c.reason {
			t.Errorf("%s: reason = %q, want %q", c.name, tr.Candidates[0].Reason, c.reason)
		}
	}
}

// Flight dates are a hard boundary at both ends.
func TestOutsideFlightDates(t *testing.T) {
	cp := base()
	cp.Campaigns[0].FlightStart = now().Add(24 * time.Hour)
	cp.Campaigns[0].FlightEnd = now().Add(48 * time.Hour)
	cp.LineItems = []LineItem{{ID: "x", CampaignID: "c1", Status: "ACTIVE", CreativeIDs: []string{"cr300"}}}
	if li, tr := decide(cp, NewMemoryBudget()); li != nil || tr.Candidates[0].Reason != ReasonOutsideFlight {
		t.Errorf("expected outside_flight_dates, got winner=%v reason=%q", li, tr.Candidates[0].Reason)
	}
}

// Budget exhaustion removes a line item from eligibility, and a zero budget
// means unlimited -- which is how house/fallback demand always fills.
func TestBudgetExhaustionAndHouseFallback(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{
		{ID: "paid", CampaignID: "c1", Status: "ACTIVE", Priority: 5,
			PricingModel: "CPM", Rate: 10.00, DailyBudget: 1.00, CreativeIDs: []string{"cr300"}},
		{ID: "house", CampaignID: "c1", Status: "ACTIVE", Priority: 0,
			PricingModel: "CPM", Rate: 0, DailyBudget: 0, CreativeIDs: []string{"cr300"}},
	}
	b := NewMemoryBudget()
	if li, _ := decide(cp, b); li == nil || li.ID != "paid" {
		t.Fatalf("with budget available the paid line item should win, got %v", li)
	}
	b.AddSpend("paid", 1.00) // exhaust it
	li, tr := decide(cp, b)
	if li == nil || li.ID != "house" {
		t.Fatalf("with budget exhausted the house fallback should fill, got %v", li)
	}
	for _, c := range tr.Candidates {
		if c.LineItemID == "paid" && c.Reason != ReasonBudgetExhausted {
			t.Errorf("paid reason = %q, want budget_exhausted", c.Reason)
		}
	}
}

// The trace must explain every candidate, not just the winner.
func TestTraceExplainsEveryCandidate(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{
		{ID: "win", CampaignID: "c1", Status: "ACTIVE", Priority: 5, PricingModel: "CPM", Rate: 5, CreativeIDs: []string{"cr300"}},
		{ID: "lost_value", CampaignID: "c1", Status: "ACTIVE", Priority: 5, PricingModel: "CPM", Rate: 2, CreativeIDs: []string{"cr300"}},
		{ID: "lost_prio", CampaignID: "c1", Status: "ACTIVE", Priority: 1, PricingModel: "CPM", Rate: 99, CreativeIDs: []string{"cr300"}},
		{ID: "rejected", CampaignID: "c1", Status: "ACTIVE", Priority: 9, CreativeIDs: []string{"cr300"},
			Targeting: Targeting{Countries: []string{"US"}}},
	}
	_, tr := decide(cp, NewMemoryBudget())
	want := map[string]string{
		"win": ReasonSelected, "lost_value": ReasonLostOnValue,
		"lost_prio": ReasonLostOnPriority, "rejected": ReasonGeoMismatch,
	}
	if len(tr.Candidates) != 4 {
		t.Fatalf("expected 4 candidates in the trace, got %d", len(tr.Candidates))
	}
	for _, c := range tr.Candidates {
		if want[c.LineItemID] != c.Reason {
			t.Errorf("%s: reason = %q, want %q", c.LineItemID, c.Reason, want[c.LineItemID])
		}
	}
}

func TestNoEligibleCandidates(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{{ID: "x", CampaignID: "c1", Status: "PAUSED", CreativeIDs: []string{"cr300"}}}
	if li, tr := decide(cp, NewMemoryBudget()); li != nil || tr.DecisionRule != "no_eligible_candidates" {
		t.Errorf("expected no_eligible_candidates, got %v / %q", li, tr.DecisionRule)
	}
}
