package main

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// The whole product thesis in one test: with only the shipped control plane and
// no knowledge of which fields any buyer needs, splitting traffic on
// field.user_eids and measuring bid rate per arm RECOVERS the dependency.
//
// A real DSP never tells you this. You find it by testing, and this is what
// finding it looks like.
func TestFieldExperimentDiscoversBuyerDependency(t *testing.T) {
	cp, err := loadControlPlane("fixtures/control-plane.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	exp := Experiments{Set: cp.Experiments}
	if !exp.has(FieldExpKey("user_eids")) {
		t.Skip("field.user_eids is not seeded in the shipped control plane")
	}

	type arm struct{ calls, bids int }
	byArm := map[string]*arm{}
	// Per buyer as well as overall. Measuring only the aggregate is the trap
	// this test exists to demonstrate -- see the assertion at the end.
	byBuyerArm := map[string]map[string]*arm{}
	rng := rand.New(rand.NewSource(99))
	now := time.Now()

	for i := 0; i < 6000; i++ {
		// Placement-assigned, so vary the placement to sample both arms.
		req := AdRequest{
			PlacementID: fmt.Sprintf("game_sidebar_%d", i%400),
			Game:        "xo", DeviceType: "mobile", Country: "IL",
			SessionID: fmt.Sprintf("s-%d", i),
		}
		fields, assigns := ResolveFields(exp, req, fmt.Sprintf("r-%d", i))

		var armID string
		for _, a := range assigns {
			if a.Key == FieldExpKey("user_eids") {
				armID = a.VariantID
			}
		}
		if armID == "" {
			t.Fatal("no arm assigned for field.user_eids")
		}

		res := runAuction(auctionInput{
			buyers: cp.Buyers, req: &req, pl: &cp.Placements[0], floor: 0,
			tmax: 200, fields: fields, budget: NewMemoryBudget(), cp: cp,
			rng: rng, now: now,
		})

		a := byArm[armID]
		if a == nil {
			a = &arm{}
			byArm[armID] = a
		}
		a.calls += res.BuyersCalled
		a.bids += res.BidsReceived

		for _, bid := range res.Bids {
			if byBuyerArm[bid.BuyerID] == nil {
				byBuyerArm[bid.BuyerID] = map[string]*arm{}
			}
			ba := byBuyerArm[bid.BuyerID][armID]
			if ba == nil {
				ba = &arm{}
				byBuyerArm[bid.BuyerID][armID] = ba
			}
			ba.calls++
			if bid.CPM > 0 {
				ba.bids++
			}
		}
	}

	send, omit := byArm["send"], byArm["omit"]
	if send == nil || omit == nil {
		t.Fatalf("both arms should have traffic, got %v", byArm)
	}

	sendRate := float64(send.bids) / float64(send.calls)
	omitRate := float64(omit.bids) / float64(omit.calls)
	drop := 1 - omitRate/sendRate

	t.Logf("arm send: %d bids / %d calls = %.1f%% bid rate", send.bids, send.calls, sendRate*100)
	t.Logf("arm omit: %d bids / %d calls = %.1f%% bid rate", omit.bids, omit.calls, omitRate*100)
	t.Logf("DISCOVERED: omitting user.eids costs %.1f%% of overall bid rate", drop*100)

	if drop <= 0.03 {
		t.Fatalf("the experiment failed to detect a seeded dependency (drop %.3f)", drop)
	}

	// --- the lesson ---
	//
	// The aggregate understates the effect badly, because only one of five
	// buyers depends on the field and the other four dilute it. Measuring
	// bid rate overall would make a 55% dependency look like noise, and a
	// product that reports the aggregate would conclude the field does not
	// matter and drop it.
	//
	// Field experiments MUST be measured per buyer.
	var worst float64
	var worstBuyer string
	for buyer, arms := range byBuyerArm {
		s, o := arms["send"], arms["omit"]
		if s == nil || o == nil || s.calls < 100 || o.calls < 100 {
			continue
		}
		sr := float64(s.bids) / float64(s.calls)
		or := float64(o.bids) / float64(o.calls)
		if sr == 0 {
			continue
		}
		d := 1 - or/sr
		t.Logf("  per-buyer %-16s send %.1f%% -> omit %.1f%%  (%.1f%% drop)",
			buyer, sr*100, or*100, d*100)
		if d > worst {
			worst, worstBuyer = d, buyer
		}
	}
	t.Logf("worst-affected buyer: %s at %.1f%% -- against %.1f%% in the aggregate",
		worstBuyer, worst*100, drop*100)

	if worst < 0.35 {
		t.Fatalf("per-buyer measurement recovered only %.1f%%; the seeded "+
			"dependency is 55%% and per-buyer analysis must find it", worst*100)
	}
	if worst <= drop*2 {
		t.Errorf("per-buyer (%.1f%%) should be far larger than aggregate (%.1f%%) -- "+
			"if not, this test no longer demonstrates the dilution trap", worst*100, drop*100)
	}
}
