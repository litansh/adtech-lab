package main

import "testing"

func campaigns() []Campaign {
	return []Campaign{{
		ID: "c1", MaxCPM: 4.0, DailyBudget: 10.0,
		Countries: []string{"IL"}, Sizes: []string{"300x250"},
		FreqCapPerUser: 3, CreativeHTML: "<div></div>",
	}}
}

func req() BidRequest {
	return BidRequest{
		ID:     "r1",
		Imp:    []Imp{{ID: "1", BidFloor: 0.5, Banner: &Banner{W: 300, H: 250}}},
		Device: &Device{Geo: &Geo{Country: "IL"}},
		User:   &User{ID: "u1"},
		TMax:   100,
	}
}

func TestBidsOnAMatchingRequest(t *testing.T) {
	b := NewBidder(campaigns(), 1, "http://127.0.0.1:8081")
	d := b.Decide(req())
	if d.Bid == nil {
		t.Fatalf("no bid: %s", d.Reason)
	}
	if d.Bid.Price <= 0 || d.Bid.Price > 4.0 {
		t.Errorf("price %.2f outside (0, max_cpm]", d.Bid.Price)
	}
	if d.Bid.NURL == "" {
		t.Error("no win notice url; the buyer can never learn it won")
	}
}

func TestDeclinesOnGeoAndSize(t *testing.T) {
	b := NewBidder(campaigns(), 1, "http://127.0.0.1:8081")
	r := req()
	r.Device.Geo.Country = "DE"
	if d := b.Decide(r); d.Bid != nil || d.Reason != NoBidGeo {
		t.Errorf("wrong country: got bid=%v reason=%s", d.Bid != nil, d.Reason)
	}
	r = req()
	r.Imp[0].Banner = &Banner{W: 728, H: 90}
	if d := b.Decide(r); d.Bid != nil || d.Reason != NoBidSize {
		t.Errorf("wrong size: got bid=%v reason=%s", d.Bid != nil, d.Reason)
	}
}

// Bidding under a disclosed floor is spending money to be rejected.
func TestNeverBidsBelowADisclosedFloor(t *testing.T) {
	b := NewBidder(campaigns(), 1, "http://127.0.0.1:8081")
	r := req()
	r.Imp[0].BidFloor = 9.99 // above max_cpm
	for i := 0; i < 200; i++ {
		if d := b.Decide(r); d.Bid != nil {
			t.Fatalf("bid $%.2f under a floor of $9.99", d.Bid.Price)
		}
	}
}

// The buyer's budget is ITS budget. The seller has no idea what it is.
func TestBudgetStopsBiddingButOnlyAfterWinsAreNotified(t *testing.T) {
	cs := campaigns()
	cs[0].DailyBudget = 0.01 // ~2 impressions at $4 CPM
	cs[0].FreqCapPerUser = 0
	b := NewBidder(cs, 1, "http://127.0.0.1:8081")

	// Bids alone never exhaust a budget: spend only moves on a WIN notice.
	for i := 0; i < 20; i++ {
		if d := b.Decide(req()); d.Bid == nil {
			t.Fatalf("stopped bidding after %d bids with no wins notified", i)
		}
	}
	for i := 0; i < 5; i++ {
		b.NotifyWin("c1", "u1", 4.00)
	}
	if d := b.Decide(req()); d.Bid != nil || d.Reason != NoBidBudget {
		t.Fatalf("kept bidding past budget: bid=%v reason=%s", d.Bid != nil, d.Reason)
	}
}

// Buyer-side capping. The SELLER also caps, on its own terms, and neither knows
// the other's state -- so a person experiences the tighter of two caps that
// nobody computed together.
func TestBuyerSideFrequencyCap(t *testing.T) {
	b := NewBidder(campaigns(), 1, "http://127.0.0.1:8081")
	for i := 0; i < 3; i++ {
		if d := b.Decide(req()); d.Bid == nil {
			t.Fatalf("capped early at %d, cap is 3", i)
		}
		b.NotifyWin("c1", "u1", 4.00)
	}
	if d := b.Decide(req()); d.Bid != nil || d.Reason != NoBidFreqCap {
		t.Fatalf("served past the cap: bid=%v reason=%s", d.Bid != nil, d.Reason)
	}
	// A different user is unaffected.
	r := req()
	r.User.ID = "u2"
	if d := b.Decide(r); d.Bid == nil {
		t.Fatalf("a different user inherited u1's cap: %s", d.Reason)
	}
}

// The one no-bid that is not about money.
func TestDeclinesWithoutConsentWhereGDPRApplies(t *testing.T) {
	b := NewBidder(campaigns(), 1, "http://127.0.0.1:8081")
	r := req()
	r.Regs = &Regs{GDPR: 1}
	if d := b.Decide(r); d.Bid != nil || d.Reason != NoBidNoConsent {
		t.Fatalf("bid under GDPR with no consent string: bid=%v reason=%s", d.Bid != nil, d.Reason)
	}
	r.Regs.GPP = "DBABLA~BVQqAAAACgA.QA"
	if d := b.Decide(r); d.Bid == nil {
		t.Fatalf("declined a consented request: %s", d.Reason)
	}
}

// The heart of Phase 4: bids submitted minus wins notified is NOT losses.
func TestBidsMinusWinsIsNotLosses(t *testing.T) {
	b := NewBidder(campaigns(), 1, "http://127.0.0.1:8081")
	cs := b.campaigns
	cs[0].FreqCapPerUser = 0
	for i := 0; i < 10; i++ {
		if d := b.Decide(req()); d.Bid == nil {
			t.Fatal("stopped bidding unexpectedly")
		}
	}
	// The seller says we won six. Two notices are lost in transit.
	for i := 0; i < 4; i++ {
		b.NotifyWin("c1", "u1", 4.00)
	}

	s := b.Stats()[0]
	if s.BidsSubmitted != 10 || s.WinsNotified != 4 {
		t.Fatalf("bids=%d wins=%d, want 10 and 4", s.BidsSubmitted, s.WinsNotified)
	}
	// The buyer believes it won 4. The seller believes it served 6. Neither is
	// lying, and no amount of reconciliation makes these agree without a shared
	// event id -- which is exactly what docs/reporting-discrepancy.md is about.
	if s.WinRate != 0.4 {
		t.Errorf("win rate %.2f, want 0.40 -- the buyer's view, not the truth", s.WinRate)
	}
}
