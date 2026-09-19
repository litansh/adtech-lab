package main

import (
	"math/rand"
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// Delivery decisioning.
//
// The order below is the lesson. A naive ad server ranks everything by eCPM and
// gets the wrong answer, because price is the LAST consideration, not the first:
//
//   1. eligibility    can this line item serve this opportunity at all?
//   2. priority       guaranteed demand outranks non-guaranteed, regardless of price
//   3. economic value expected_ecpm, normalised across pricing models
//
// Google Ad Manager's dynamic allocation is the industry example: real-time
// demand competes with *remnant* line items on price, while guaranteed line
// items are protected by priority and delivery obligation. A $15 bid does not
// automatically beat a $12 guaranteed sponsorship.
// ---------------------------------------------------------------------------

// Reasons a candidate was rejected. Enumerated rather than free text so the
// Decision Trace is queryable and rejections can be counted over time.
const (
	ReasonSelected           = "selected"
	ReasonCampaignPaused     = "campaign_not_active"
	ReasonLineItemPaused     = "line_item_not_active"
	ReasonOutsideFlight      = "outside_flight_dates"
	ReasonBudgetExhausted    = "budget_exhausted"
	ReasonFrequencyCapped    = "frequency_capped"
	ReasonPacedDown          = "paced_down"
	ReasonNoCreativeThatFits = "no_creative_for_slot"
	ReasonGeoMismatch        = "geo_mismatch"
	ReasonDeviceMismatch     = "device_mismatch"
	ReasonGameMismatch       = "game_mismatch"
	ReasonPlacementMismatch  = "placement_mismatch"
	ReasonLostOnPriority     = "lost_on_priority"
	ReasonLostOnValue        = "lost_on_expected_ecpm"
)

// Candidate is one line item evaluated against one opportunity.
type Candidate struct {
	LineItemID   string  `json:"line_item_id"`
	LineItemName string  `json:"line_item_name"`
	Eligible     bool    `json:"eligible"`
	Reason       string  `json:"reason"`
	Priority     int     `json:"priority,omitempty"`
	PricingModel string  `json:"pricing_model,omitempty"`
	Rate         float64 `json:"rate,omitempty"`
	ExpectedECPM float64 `json:"expected_ecpm,omitempty"`
	SpendToday   float64 `json:"spend_today,omitempty"`
	DailyBudget  float64 `json:"daily_budget,omitempty"`
	CreativeID   string  `json:"creative_id,omitempty"`

	// Frequency, when a cap applies. Present so the Trace can answer "why did
	// this NOT serve?" with a number rather than a category.
	FrequencySeen int `json:"frequency_seen,omitempty"`
	FrequencyCap  int `json:"frequency_cap,omitempty"`

	// Pacing, when a throttle applied. Present so "why did this not serve?"
	// answers with the numbers rather than a category.
	Pacing *PacingDecision `json:"pacing,omitempty"`
}

// Trace answers "why did this request produce this ad?" -- the single most
// valuable artefact in this project. Lab only; never returned in production.
type Trace struct {
	RequestID    string      `json:"request_id"`
	PlacementID  string      `json:"placement_id"`
	Country      string      `json:"country"`
	DeviceType   string      `json:"device_type"`
	Game         string      `json:"game"`
	Candidates   []Candidate `json:"candidates"`
	Winner       string      `json:"winner,omitempty"`
	DecisionRule string      `json:"decision_rule"`
	LatencyMS    float64     `json:"latency_ms"`
}

// expectedECPM normalises every pricing model to value per 1000 impressions so
// they can be compared. Without this you cannot rank mixed demand -- and you
// cannot later build an auction, because an auction is just this comparison
// across independent buyers.
//
//	CPM  the rate already is per 1000 impressions
//	CPC  rate x expected clicks per impression x 1000
//	CPA  rate x expected conversions per impression x 1000
func expectedECPM(li *LineItem) float64 {
	switch li.PricingModel {
	case "CPM":
		return li.Rate
	case "CPC":
		return li.Rate * li.ExpectedCTR * 1000
	case "CPA":
		return li.Rate * li.ExpectedCTR * li.ExpectedCVR * 1000
	default:
		return 0
	}
}

func matches(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true // no constraint on this dimension
	}
	for _, a := range allowed {
		if a == actual {
			return true
		}
	}
	return false
}

// eligible runs the hard checks in ascending order of cost: cheap in-memory
// field comparisons first, state lookups last. Ordering changes only the cost
// of evaluation, never the result -- every check must pass regardless.
func eligible(li *LineItem, cp *ControlPlane, req *AdRequest, pl *Placement,
	spend float64, now time.Time) (bool, string, string) {

	if li.Status != "ACTIVE" {
		return false, ReasonLineItemPaused, ""
	}
	c := cp.campaign(li.CampaignID)
	if c == nil || c.Status != "ACTIVE" {
		return false, ReasonCampaignPaused, ""
	}
	if now.Before(c.FlightStart) || now.After(c.FlightEnd) {
		return false, ReasonOutsideFlight, ""
	}
	if !matches(li.Targeting.Countries, req.Country) {
		return false, ReasonGeoMismatch, ""
	}
	if !matches(li.Targeting.DeviceTypes, req.DeviceType) {
		return false, ReasonDeviceMismatch, ""
	}
	if !matches(li.Targeting.Games, req.Game) {
		return false, ReasonGameMismatch, ""
	}
	if !matches(li.Targeting.Placements, req.PlacementID) {
		return false, ReasonPlacementMismatch, ""
	}

	// A creative that physically fits the slot. Obvious, and a real source of
	// silent under-delivery when it is missing.
	creativeID := ""
	for _, cid := range li.CreativeIDs {
		if cr := cp.creative(cid); cr != nil && cr.Width == pl.Width && cr.Height == pl.Height {
			creativeID = cr.ID
			break
		}
	}
	if creativeID == "" {
		return false, ReasonNoCreativeThatFits, ""
	}

	// Budget is checked against serving state, which is deliberately allowed to
	// be approximate. See docs/architecture-proposal.md: bounded over-delivery
	// is accepted here; the billing ledger is the durable record.
	// A zero DailyBudget means unlimited -- used by house/fallback demand.
	if li.DailyBudget > 0 && spend >= li.DailyBudget {
		return false, ReasonBudgetExhausted, ""
	}

	return true, "", creativeID
}

// Decide evaluates every line item and picks a winner.
func Decide(cp *ControlPlane, req *AdRequest, pl *Placement, budget BudgetState,
	freq FrequencyState, rng *rand.Rand, now time.Time) (*LineItem, string, *Trace) {
	tr := &Trace{
		RequestID:   "",
		PlacementID: req.PlacementID,
		Country:     req.Country,
		DeviceType:  req.DeviceType,
		Game:        req.Game,
	}

	type scored struct {
		li         *LineItem
		creativeID string
		ecpm       float64
	}
	var winners []scored

	for i := range cp.LineItems {
		li := &cp.LineItems[i]
		spend := budget.SpendToday(li.ID)
		ok, reason, creativeID := eligible(li, cp, req, pl, spend, now)

		// Frequency capping is checked LAST among eligibility rules, so a
		// capped line item reports "frequency_capped" rather than being
		// rejected earlier for something less specific. The Decision Trace is
		// only useful if the reason it gives is the real one.
		if ok && li.FrequencyCap.Capped(freq, req.SessionID, li.ID) {
			ok, reason, creativeID = false, ReasonFrequencyCapped, ""
		}

		// Pacing is checked after capping, and both after budget. The order is
		// the specificity order: "out of budget" is a harder fact than "capped
		// for this person", which is harder than "ahead of schedule right now".
		// The Trace should report the most specific true reason.
		var pace PacingDecision
		if ok {
			var serve bool
			pace, serve = Pace(li, spend, now, rng)
			if !serve {
				ok, reason, creativeID = false, ReasonPacedDown, ""
			}
		}

		cand := Candidate{
			LineItemID:   li.ID,
			LineItemName: li.Name,
			Eligible:     ok,
			Reason:       reason,
			Priority:     li.Priority,
			PricingModel: li.PricingModel,
			Rate:         li.Rate,
			SpendToday:   spend,
			DailyBudget:  li.DailyBudget,
		}
		if pace.Throttled {
			cand.Pacing = &pace
		}
		if freq != nil && li.FrequencyCap.Impressions > 0 {
			cand.FrequencySeen = freq.Count(req.SessionID, li.ID)
			cand.FrequencyCap = li.FrequencyCap.Impressions
		}
		if ok {
			cand.ExpectedECPM = expectedECPM(li)
			cand.CreativeID = creativeID
			winners = append(winners, scored{li, creativeID, cand.ExpectedECPM})
		}
		tr.Candidates = append(tr.Candidates, cand)
	}

	if len(winners) == 0 {
		tr.DecisionRule = "no_eligible_candidates"
		return nil, "", tr
	}

	// Priority first, then expected value. This ordering is the whole point.
	sort.SliceStable(winners, func(a, b int) bool {
		if winners[a].li.Priority != winners[b].li.Priority {
			return winners[a].li.Priority > winners[b].li.Priority
		}
		return winners[a].ecpm > winners[b].ecpm
	})

	win := winners[0]
	tr.Winner = win.li.ID

	// Name the rule that actually decided it, so the trace teaches rather than
	// merely reports. If the runner-up had equal priority, value broke the tie.
	tr.DecisionRule = "only_eligible_candidate"
	if len(winners) > 1 {
		if winners[1].li.Priority < win.li.Priority {
			tr.DecisionRule = "highest_priority"
		} else {
			tr.DecisionRule = "highest_expected_ecpm"
		}
	}

	// Annotate the losers so the trace explains every outcome, not just the win.
	for i := range tr.Candidates {
		c := &tr.Candidates[i]
		if !c.Eligible {
			continue
		}
		if c.LineItemID == win.li.ID {
			c.Reason = ReasonSelected
		} else if c.Priority < win.li.Priority {
			c.Reason = ReasonLostOnPriority
		} else {
			c.Reason = ReasonLostOnValue
		}
	}

	return win.li, win.creativeID, tr
}
