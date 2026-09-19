package main

import (
	"math/rand"
	"testing"
)

func buyersFixture() []Buyer {
	return []Buyer{
		{ID: "b-high", Name: "High bidder", BaseCPM: 8.00, Variance: 0, BidRate: 1, CreativeIDs: []string{"cr300"}},
		{ID: "b-mid", Name: "Mid bidder", BaseCPM: 5.00, Variance: 0, BidRate: 1, CreativeIDs: []string{"cr300"}},
		{ID: "b-low", Name: "Low bidder", BaseCPM: 1.00, Variance: 0, BidRate: 1, CreativeIDs: []string{"cr300"}},
	}
}

func auc(t *testing.T, buyers []Buyer, floor float64, tmax int, seed int64) *AuctionResult {
	t.Helper()
	cp := base()
	cp.Buyers = buyers
	return runAuction(auctionInput{
		buyers: buyers, req: req(), pl: &cp.Placements[0], floor: floor, tmax: tmax,
		budget: NewMemoryBudget(), cp: cp, rng: rand.New(rand.NewSource(seed)), now: now(),
	})
}

// FIRST price: the winner pays its own bid, NOT the runner-up's. Under second
// price the winner would pay $5.00 here. That difference is the whole reason
// bid shading exists.
func TestFirstPriceWinnerPaysItsOwnBid(t *testing.T) {
	r := auc(t, buyersFixture(), 0, 1000, 1)
	if r.Winner == nil || r.Winner.BuyerID != "b-high" {
		t.Fatalf("expected b-high to win, got %+v", r.Winner)
	}
	if r.ClearingPrice != 8.00 {
		t.Errorf("clearing price = %.2f, want 8.00 (its own bid, not the $5.00 runner-up)", r.ClearingPrice)
	}
}

// A floor filters bids. This is the thing that could not be tested before there
// were competing buyers -- with one buyer a floor has nothing to filter.
func TestFloorRejectsBidsBeneathIt(t *testing.T) {
	r := auc(t, buyersFixture(), 6.00, 1000, 1)
	if r.Winner == nil || r.Winner.BuyerID != "b-high" {
		t.Fatalf("expected b-high to survive a $6.00 floor, got %+v", r.Winner)
	}
	if r.BelowFloor != 2 {
		t.Errorf("below_floor = %d, want 2 ($5.00 and $1.00 are both under $6.00)", r.BelowFloor)
	}
	// And the floor/fill trade-off, in one assertion: raise it past everyone.
	r2 := auc(t, buyersFixture(), 9.00, 1000, 1)
	if r2.Winner != nil {
		t.Errorf("a $9.00 floor should clear the field, got winner %+v", r2.Winner)
	}
	if r2.Reason != "no_valid_bids" {
		t.Errorf("reason = %q, want no_valid_bids", r2.Reason)
	}
}

// tmax is eligibility, not performance tuning. A buyer that answers late was
// never in the auction -- it did not lose it.
func TestTimeoutMeansNeverInTheAuction(t *testing.T) {
	b := buyersFixture()
	b[0].LatencyMS = 500 // the highest bidder is slow
	r := auc(t, b, 0, 100, 1)

	if r.Winner == nil || r.Winner.BuyerID != "b-mid" {
		t.Fatalf("the slow $8.00 buyer must not win; got %+v", r.Winner)
	}
	if r.ClearingPrice != 5.00 {
		t.Errorf("clearing = %.2f, want 5.00", r.ClearingPrice)
	}
	if r.TimedOut != 1 {
		t.Errorf("timed_out = %d, want 1", r.TimedOut)
	}
	for _, bid := range r.Bids {
		if bid.BuyerID == "b-high" && bid.Status != BidTimeout {
			t.Errorf("b-high status = %q, want timeout", bid.Status)
		}
	}
}

// Bid density: buyers decline for reasons the seller cannot see. A population
// where everyone always bids is a sorted list, not a marketplace.
func TestBidDensityIsBelowBuyersCalled(t *testing.T) {
	b := buyersFixture()
	for i := range b {
		b[i].BidRate = 0.5
	}
	seen := map[int]bool{}
	for seed := int64(1); seed <= 40; seed++ {
		r := auc(t, b, 0, 1000, seed)
		seen[r.BidsReceived] = true
		if r.BidsReceived > r.BuyersCalled {
			t.Fatalf("bids_received %d > buyers_called %d", r.BidsReceived, r.BuyersCalled)
		}
	}
	if len(seen) < 2 {
		t.Error("bid density never varied across 40 auctions; the no-bid path is not exercised")
	}
}

// Every buyer's outcome is recorded, including those that never bid -- "why was
// there no demand?" is asked more often than "who won?".
func TestEveryBuyerAppearsInTheRecord(t *testing.T) {
	b := buyersFixture()
	b[2].Targeting = Targeting{Countries: []string{"US"}} // excluded before bidding
	r := auc(t, b, 0, 1000, 1)

	if len(r.Bids) != 3 {
		t.Fatalf("expected all 3 buyers recorded, got %d", len(r.Bids))
	}
	for _, bid := range r.Bids {
		if bid.BuyerID == "b-low" {
			if bid.Status != BidNotEligible || bid.Reason != ReasonGeoMismatch {
				t.Errorf("b-low = %q/%q, want not_eligible/geo_mismatch", bid.Status, bid.Reason)
			}
		}
	}
}

// --------------------------------------------------------------------------
// Dynamic allocation
// --------------------------------------------------------------------------

// A $15 bid does NOT beat a $12 guarantee. Priority is a different question,
// asked first -- not a tiebreaker.
func TestGuaranteedBeatsAHigherBid(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{{ID: "guaranteed", CampaignID: "c1", Status: "ACTIVE",
		Priority: 10, PricingModel: "CPM", Rate: 12.00, CreativeIDs: []string{"cr300"}}}
	cp.Buyers = []Buyer{{ID: "rich", Name: "Rich", BaseCPM: 15.00, Variance: 0, BidRate: 1, CreativeIDs: []string{"cr300"}}}

	a := Allocate(cp, req(), &cp.Placements[0], NewMemoryBudget(), 0, 1000, nil, nil, nil, rand.New(rand.NewSource(1)), now())
	if a.Source != "guaranteed" || a.LineItemID != "guaranteed" {
		t.Fatalf("guaranteed demand must win outright, got source=%q id=%q", a.Source, a.LineItemID)
	}
}

// ...but the same $15 bid absolutely beats a $12 REMNANT.
func TestAuctionBeatsRemnantOnPrice(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{{ID: "remnant", CampaignID: "c1", Status: "ACTIVE",
		Priority: 1, PricingModel: "CPM", Rate: 12.00, CreativeIDs: []string{"cr300"}}}
	cp.Buyers = []Buyer{{ID: "rich", Name: "Rich", BaseCPM: 15.00, Variance: 0, BidRate: 1, CreativeIDs: []string{"cr300"}}}

	a := Allocate(cp, req(), &cp.Placements[0], NewMemoryBudget(), 0, 1000, nil, nil, nil, rand.New(rand.NewSource(1)), now())
	if a.Source != "auction" || a.BuyerID != "rich" {
		t.Fatalf("a $15 bid must beat a $12 remnant, got source=%q", a.Source)
	}
	if a.PriceCPM != 15.00 {
		t.Errorf("price = %.2f, want 15.00", a.PriceCPM)
	}
}

// And when no bid clears, remnant demand still fills the slot.
func TestRemnantFillsWhenNoBidClears(t *testing.T) {
	cp := base()
	cp.LineItems = []LineItem{{ID: "remnant", CampaignID: "c1", Status: "ACTIVE",
		Priority: 1, PricingModel: "CPM", Rate: 2.00, CreativeIDs: []string{"cr300"}}}
	cp.Buyers = []Buyer{{ID: "cheap", Name: "Cheap", BaseCPM: 0.50, Variance: 0, BidRate: 1, CreativeIDs: []string{"cr300"}}}

	a := Allocate(cp, req(), &cp.Placements[0], NewMemoryBudget(), 1.00, 1000, nil, nil, nil, rand.New(rand.NewSource(1)), now())
	if a.Source != "remnant" {
		t.Fatalf("remnant should fill when every bid is below the floor, got %q", a.Source)
	}
	if a.Auction.BelowFloor != 1 {
		t.Errorf("below_floor = %d, want 1", a.Auction.BelowFloor)
	}
}

// The Demand agent asks "how is THIS buyer doing?", and the aggregate cannot
// answer it: 23% of calls timing out does not say that one buyer accounts for
// all of them. So every buyer's outcome is logged, not just the summary.
func TestPerBuyerOutcomesAreLogged(t *testing.T) {
	s := testServer()
	s.lab = true
	ev := map[string]any{}
	alloc := &Allocation{Auction: &AuctionResult{
		BuyersCalled: 3, BidsReceived: 2,
		Bids: []Bid{
			{BuyerID: "a", Status: BidPlaced, CPM: 4.5, LatencyMS: 30},
			{BuyerID: "b", Status: BidTimeout, LatencyMS: 250},
			{BuyerID: "c", Status: BidNoBid, LatencyMS: 20},
		},
		Winner: &Bid{BuyerID: "a", CPM: 4.5},
	}}
	s.addAuction(ev, alloc)

	rows, ok := ev["auction_buyers"].([]map[string]any)
	if !ok || len(rows) != 3 {
		t.Fatalf("per-buyer rows missing or wrong length: %#v", ev["auction_buyers"])
	}
	byBuyer := map[string]map[string]any{}
	for _, r := range rows {
		byBuyer[r["b"].(string)] = r
	}
	if byBuyer["b"]["s"] != BidTimeout {
		t.Errorf("timeout not recorded for buyer b: %v", byBuyer["b"])
	}
	if byBuyer["b"]["l"].(int) != 250 {
		t.Errorf("a timing-out buyer's latency is the interesting part: %v", byBuyer["b"])
	}
	if _, has := byBuyer["c"]["p"]; has {
		t.Error("a no-bid should carry no price")
	}
	if byBuyer["a"]["w"] != 1 {
		t.Error("the winner is not marked, so win rate cannot be computed per buyer")
	}
}
