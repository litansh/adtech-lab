package main

import (
	"math"
	"sort"
)

// ---------------------------------------------------------------------------
// The Demand agent: which buyers are worth calling.
//
// Yield decides what PRICE we accept. Demand decides WHO WE ASK, and those are
// different levers with different economics. The measurement that motivates
// this one:
//
//   buyer calls are 37.8% of infrastructure cost per thousand requests -- more
//   than CloudFront, API Gateway and Lambda combined. Fan-out to demand costs
//   seven times more than compute.
//
// So a buyer that never answers in time is not a neutral disappointment. It is
// the most expensive possible outcome: we pay the full tmax of held compute and
// a round trip, and get nothing. buyer-slow bids the highest at $11.00, times
// out on 75% of calls, and wins nothing -- invisible to any revenue-based view,
// and the single most expensive thing in the auction.
//
// The decision is economic and per buyer:
//
//   value_per_call = win_rate x clearing_price/1000 x take_rate  -  cost_per_call
//
// Negative means we lose money by asking. But "stop asking" is the wrong action
// for the same reason a paced-down line item never drops to zero: a buyer we
// never call produces no observations, so we can never learn it recovered.
// ---------------------------------------------------------------------------

// BuyerStats is what the event log says about one buyer over a period.
type BuyerStats struct {
	BuyerID   string
	Calls     int
	Bids      int
	Timeouts  int
	Wins      int
	Revenue   float64 // our revenue from this buyer's wins
	LatencyMS []int   // per call, for percentiles
}

// BuyerEconomics is the decision input.
type BuyerEconomics struct {
	BuyerID      string  `json:"buyer_id"`
	Calls        int     `json:"calls"`
	BidRate      float64 `json:"bid_rate"`
	TimeoutRate  float64 `json:"timeout_rate"`
	WinRate      float64 `json:"win_rate"`
	RevenueUSD   float64 `json:"revenue_usd"`
	CostUSD      float64 `json:"cost_usd"`
	ValuePerCall float64 `json:"value_per_call"`
	P95LatencyMS int     `json:"p95_latency_ms"`
	// ShareOfTimeouts is the share of ALL timeouts this buyer caused. An
	// aggregate timeout rate hides the case where one buyer is the whole
	// problem, which is the usual case.
	ShareOfTimeouts float64 `json:"share_of_timeouts"`
}

// CostPerCall is the measured infrastructure cost of asking one buyer, from
// docs/finops.md: egress out and back, plus the compute held while waiting.
const CostPerCall = 0.00000068 // $0.002040 per 1000 calls / 3 calls per request

func Analyse(stats []BuyerStats) []BuyerEconomics {
	totalTimeouts := 0
	for _, s := range stats {
		totalTimeouts += s.Timeouts
	}

	out := make([]BuyerEconomics, 0, len(stats))
	for _, s := range stats {
		e := BuyerEconomics{
			BuyerID: s.BuyerID, Calls: s.Calls, RevenueUSD: s.Revenue,
			CostUSD: float64(s.Calls) * CostPerCall,
		}
		if s.Calls > 0 {
			e.BidRate = float64(s.Bids) / float64(s.Calls)
			e.TimeoutRate = float64(s.Timeouts) / float64(s.Calls)
			e.WinRate = float64(s.Wins) / float64(s.Calls)
			e.ValuePerCall = (s.Revenue - e.CostUSD) / float64(s.Calls)
		}
		if totalTimeouts > 0 {
			e.ShareOfTimeouts = float64(s.Timeouts) / float64(totalTimeouts)
		}
		e.P95LatencyMS = percentile(s.LatencyMS, 0.95)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ValuePerCall < out[j].ValuePerCall })
	return out
}

func percentile(xs []int, p float64) int {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int(nil), xs...)
	sort.Ints(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(s) {
		i = len(s) - 1
	}
	return s[i]
}

// Proposal is what to do about one buyer.
type Proposal struct {
	BuyerID   string  `json:"buyer_id"`
	CallRate  float64 `json:"call_rate"` // 0..1, share of eligible opportunities
	Reason    string  `json:"reason"`
	WasCalled float64 `json:"was_call_rate"`
}

const (
	// MinCallRate is why "stop calling" is never the action. A buyer we never
	// ask produces no observations, so we can never learn that it recovered --
	// the same reason a paced-down line item never drops to zero.
	MinCallRate = 0.05

	// MinCallsToJudge: below this, a bad rate is a small sample rather than a
	// fact about the buyer.
	MinCallsToJudge = 200

	// MaxMovePerRun bounds how fast the call rate changes, so one bad hour
	// cannot silence a buyer.
	MaxMovePerRun = 0.20
)

// Propose decides each buyer's call rate.
//
// current may be nil, meaning everyone is called on every eligible opportunity.
func Propose(econ []BuyerEconomics, current map[string]float64) []Proposal {
	out := make([]Proposal, 0, len(econ))
	for _, e := range econ {
		was := 1.0
		if current != nil {
			if v, ok := current[e.BuyerID]; ok {
				was = v
			}
		}
		p := Proposal{BuyerID: e.BuyerID, CallRate: was, WasCalled: was}

		switch {
		case e.Calls < MinCallsToJudge:
			p.Reason = "too few calls to judge; unchanged"

		case e.ValuePerCall > 0:
			// Earning its calls. Restore toward full, bounded.
			p.CallRate = math.Min(1.0, was+MaxMovePerRun)
			p.Reason = "profitable per call"

		case e.TimeoutRate > 0.5:
			// The expensive failure: we pay the full tmax and get nothing.
			//
			// EXEMPT from the move limit, for the same reason a guardrail
			// breach is exempt in yieldctl. The limit exists so a noisy hour
			// cannot swing the platform; a buyer timing out on more than half
			// of several hundred calls is not a noisy hour, and draining it
			// gradually spends money on every run in between.
			p.CallRate = MinCallRate
			p.Reason = "times out on over half of calls -- pure cost"
			out = append(out, p)
			continue

		default:
			// Losing money, but answering. Throttle proportionally rather than
			// cutting off, so it keeps a chance to recover.
			p.CallRate = math.Max(MinCallRate, was-MaxMovePerRun)
			p.Reason = "negative value per call"
		}

		// Never move faster than the limit, in either direction.
		if d := p.CallRate - was; d > MaxMovePerRun {
			p.CallRate = was + MaxMovePerRun
		} else if d < -MaxMovePerRun {
			p.CallRate = was - MaxMovePerRun
		}
		if p.CallRate < MinCallRate {
			p.CallRate = MinCallRate
		}
		out = append(out, p)
	}
	return out
}

// Savings estimates what a proposal avoids, so the recommendation carries a
// number rather than an opinion.
func Savings(econ []BuyerEconomics, props []Proposal) (callsAvoided int, usdSaved, revenueAtRisk float64) {
	by := map[string]BuyerEconomics{}
	for _, e := range econ {
		by[e.BuyerID] = e
	}
	for _, p := range props {
		e := by[p.BuyerID]
		if p.CallRate >= p.WasCalled {
			continue
		}
		avoided := float64(e.Calls) * (p.WasCalled - p.CallRate)
		callsAvoided += int(avoided)
		usdSaved += avoided * CostPerCall
		// Honest about the other side: fewer calls to a buyer that sometimes
		// wins is revenue we are choosing not to pursue.
		if e.Calls > 0 {
			revenueAtRisk += e.RevenueUSD * (p.WasCalled - p.CallRate)
		}
	}
	return
}
