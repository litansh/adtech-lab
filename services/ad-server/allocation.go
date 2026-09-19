package main

import (
	"math/rand"
	"time"
)

// ---------------------------------------------------------------------------
// Dynamic allocation: how booked demand and bid demand compete.
//
// This is the piece that makes an ad server more than a sorted list, and it is
// modelled on how Google Ad Manager actually behaves:
//
//   1. GUARANTEED line items win on PRIORITY. The publisher sold a contract;
//      a higher spot bid does not release them from it. See ADR/learning notes.
//   2. Everything else -- remnant line items and live bids -- competes on PRICE
//      in a single comparison, because at that point they are the same kind of
//      demand: unbooked, and interchangeable.
//
// The subtlety worth keeping: a $15 bid does not beat a $12 guarantee, but it
// absolutely beats a $12 remnant. Priority is not a tiebreaker, it is a
// different question asked first.
// ---------------------------------------------------------------------------

// guaranteedPriority and above is treated as booked, contracted demand.
const guaranteedPriority = 5

type Allocation struct {
	Source string // "guaranteed" | "auction" | "remnant" | "none"
	// AdM and NURL are set when a real network buyer wins.
	AdM        string
	NURL       string
	LineItemID string // set for guaranteed and remnant
	BuyerID    string // set for auction
	CreativeID string
	PriceCPM   float64
	Reason     string
	Auction    *AuctionResult
	Trace      *Trace
}

func Allocate(cp *ControlPlane, req *AdRequest, pl *Placement, budget BudgetState,
	floor float64, tmax int, fields FieldSet, freq FrequencyState,
	httpBids []httpBidResult, rng *rand.Rand, now time.Time) *Allocation {

	li, creativeID, trace := Decide(cp, req, pl, budget, freq, rng, now)

	// --- 1. guaranteed demand wins outright ---
	if li != nil && li.Priority >= guaranteedPriority {
		return &Allocation{
			Source: "guaranteed", LineItemID: li.ID, CreativeID: creativeID,
			PriceCPM: expectedECPM(li), Reason: "guaranteed_priority", Trace: trace,
		}
	}

	// --- 2. the auction ---
	auc := runAuction(auctionInput{
		buyers: cp.Buyers, req: req, pl: pl, floor: floor,
		tmax: tmax, fields: fields, httpBids: httpBids,
		budget: budget, cp: cp, rng: rng, now: now,
	})

	// --- 3. the winning bid competes with remnant demand on price ---
	remnantECPM := 0.0
	if li != nil {
		remnantECPM = expectedECPM(li)
	}

	switch {
	case auc.Winner != nil && auc.ClearingPrice > remnantECPM:
		return &Allocation{
			Source: "auction", BuyerID: auc.Winner.BuyerID, CreativeID: auc.Winner.CreativeID,
			PriceCPM: auc.ClearingPrice, Reason: "auction_beat_remnant",
			AdM: auc.Winner.AdM, NURL: auc.Winner.NURL,
			Auction: auc, Trace: trace,
		}
	case li != nil:
		reason := "remnant_beat_auction"
		if auc.Winner == nil {
			reason = "no_valid_bids_remnant_filled"
		}
		return &Allocation{
			Source: "remnant", LineItemID: li.ID, CreativeID: creativeID,
			PriceCPM: remnantECPM, Reason: reason, Auction: auc, Trace: trace,
		}
	default:
		return &Allocation{Source: "none", Reason: auc.Reason, Auction: auc, Trace: trace}
	}
}
