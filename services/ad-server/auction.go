package main

import (
	"math/rand"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// First-price auction.
//
// The winner pays exactly what it bid. Header bidding made a genuinely global
// second-price auction impractical -- each exchange sees only its own demand,
// so "the second price" has no shared meaning across the whole opportunity --
// and the market moved to first-price as a result.
//
// The consequence is that the burden of not overpaying moves to the BUYER.
// A buyer bidding its full valuation captures none of the surplus from winning;
// bid shading is the attempt to bid below valuation while keeping a sufficient
// probability of winning. We do not implement shading here: Phase 4 gives a
// buyer its own brain, and shading is that buyer's problem, not the auction's.
// ---------------------------------------------------------------------------

// tmax is the auction's deadline. A buyer that answers late is not a buyer that
// lost -- it was never in the auction. This is the single most under-appreciated
// fact about RTB: latency is eligibility, not performance tuning.
const defaultTmaxMS = 100

type auctionInput struct {
	buyers []Buyer
	req    *AdRequest
	pl     *Placement
	floor  float64
	// fields is the optional bid-request fields this request carries. Buyers
	// that need an absent field bid less often -- see Buyer.FieldSensitivity.
	fields FieldSet
	// httpBids are real buyers already called over the network. They are merged
	// in rather than fetched here, so runAuction stays synchronous and testable.
	httpBids []httpBidResult
	tmax     int
	budget   BudgetState
	cp       *ControlPlane
	rng      *rand.Rand
	now      time.Time
}

// runAuction collects bids and picks a winner. Every buyer's outcome is
// recorded, including the ones that never bid, because "why was there no
// demand?" is a more common question in practice than "who won?".
func runAuction(in auctionInput) *AuctionResult {
	res := &AuctionResult{
		Floor:        in.floor,
		TmaxMS:       in.tmax,
		BuyersCalled: 0,
	}

	var valid []Bid

	// Real buyers first: they were called in parallel before the auction ran,
	// so by the time we get here their outcome is already known.
	for _, h := range in.httpBids {
		res.BuyersCalled++
		bid := Bid{BuyerID: h.BuyerID, BuyerName: h.BuyerID, LatencyMS: h.LatencyMS, NURL: h.NURL}
		switch {
		case h.TimedOut:
			bid.Status = BidTimeout
			res.TimedOut++
		case h.Err != "":
			// An error is a no-bid, not an auction failure. A buyer that is
			// down must not be able to stop the auction.
			bid.Status, bid.Reason = BidNoBid, h.Err
		case h.Price <= 0:
			bid.Status, bid.Reason = BidNoBid, h.NoBidReason
		default:
			bid.CPM = round2(h.Price)
			bid.CreativeID = h.CreativeID
			bid.AdM = h.AdM
			res.BidsReceived++
			if bid.CPM < in.floor {
				bid.Status = BidBelowFloor
				res.BelowFloor++
			} else {
				bid.Status = BidPlaced
				valid = append(valid, bid)
			}
		}
		res.Bids = append(res.Bids, bid)
	}

	for i := range in.buyers {
		b := &in.buyers[i]
		if b.Endpoint != "" {
			continue // already called over the network above
		}
		res.BuyersCalled++
		bid := Bid{BuyerID: b.ID, BuyerName: b.Name}

		// --- eligibility, before any bidding happens ---
		creativeID := ""
		for _, cid := range b.CreativeIDs {
			if cr := in.cp.creative(cid); cr != nil && cr.Width == in.pl.Width && cr.Height == in.pl.Height {
				creativeID = cr.ID
				break
			}
		}
		switch {
		case !matches(b.Targeting.Countries, in.req.Country):
			bid.Status, bid.Reason = BidNotEligible, ReasonGeoMismatch
		case !matches(b.Targeting.DeviceTypes, in.req.DeviceType):
			bid.Status, bid.Reason = BidNotEligible, ReasonDeviceMismatch
		case !matches(b.Targeting.Games, in.req.Game):
			bid.Status, bid.Reason = BidNotEligible, ReasonGameMismatch
		case creativeID == "":
			bid.Status, bid.Reason = BidNotEligible, ReasonNoCreativeThatFits
		case b.DailyBudget > 0 && in.budget.SpendToday("buyer:"+b.ID) >= b.DailyBudget:
			bid.Status, bid.Reason = BidNotEligible, ReasonBudgetExhausted
		}
		if bid.Status != "" {
			res.Bids = append(res.Bids, bid)
			continue
		}

		// --- latency: does the answer arrive before tmax? ---
		latency := b.LatencyMS
		if in.rng.Float64() < b.TimeoutRate {
			latency = in.tmax + 1 + in.rng.Intn(400) // late, by a realistic margin
		}
		bid.LatencyMS = latency
		if latency > in.tmax {
			bid.Status = BidTimeout
			res.TimedOut++
			res.Bids = append(res.Bids, bid)
			continue
		}

		// --- field sensitivity: does the request carry what this buyer needs? ---
		// A buyer cannot tell us which fields it needs. It just bids less
		// often without them, which is exactly why this is discoverable only
		// by experiment and not by reading a spec.
		effectiveBidRate := b.BidRate
		for field, loss := range b.FieldSensitivity {
			if in.fields != nil && !in.fields[field] {
				effectiveBidRate *= (1 - loss)
			}
		}

		// --- does it bid at all? ---
		// Low bid density is the norm, not the exception. A buyer declines for
		// reasons the seller cannot see: no matching campaign, pacing, its own
		// frequency caps, a user it has no data on.
		if in.rng.Float64() > effectiveBidRate {
			bid.Status = BidNoBid
			res.Bids = append(res.Bids, bid)
			continue
		}

		// --- price ---
		spread := b.BaseCPM * b.Variance
		cpm := b.BaseCPM + (in.rng.Float64()*2-1)*spread
		if cpm < 0 {
			cpm = 0
		}
		bid.CPM = round2(cpm)
		bid.CreativeID = creativeID
		res.BidsReceived++

		if bid.CPM < in.floor {
			bid.Status = BidBelowFloor
			res.BelowFloor++
			res.Bids = append(res.Bids, bid)
			continue
		}

		bid.Status = BidPlaced
		valid = append(valid, bid)
		res.Bids = append(res.Bids, bid)
	}

	if len(valid) == 0 {
		res.Reason = "no_valid_bids"
		return res
	}

	sort.SliceStable(valid, func(a, b int) bool { return valid[a].CPM > valid[b].CPM })
	win := valid[0]

	// FIRST price: the winner pays its own bid, not the runner-up's.
	res.ClearingPrice = win.CPM
	res.Winner = &win
	res.Reason = "highest_bid"

	for i := range res.Bids {
		if res.Bids[i].Status != BidPlaced {
			continue
		}
		if res.Bids[i].BuyerID == win.BuyerID {
			res.Bids[i].Status = BidWon
		} else {
			res.Bids[i].Status = BidLost
		}
	}
	return res
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
