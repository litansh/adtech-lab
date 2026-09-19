package main

import (
	"math"
	"math/rand"
	"time"
)

// ---------------------------------------------------------------------------
// Pacing: spending a budget over time rather than as fast as possible.
//
// Why it exists. An advertiser buying a day of a publisher's audience does not
// want that day delivered between 09:00 and 09:40. The audience at 09:00 is not
// the audience at 21:00 -- different people, different context, different
// intent -- so front-loading does not buy a day of reach, it buys a biased
// sample of it and calls it a day.
//
// The mechanic is simple and the failure modes are not:
//
//   target(t) = daily_budget * elapsed_fraction(t)
//   if spend > target, slow down
//
// HOW you slow down is the whole design. A hard stop produces sawtooth
// delivery: spend, stop, wait, spend, stop -- and each stop is a period where
// the advertiser is absent from an auction they were willing to win. So the
// throttle is PROBABILISTIC: past target we keep serving, at a reduced rate,
// and delivery stays smooth.
//
// The floor matters as much as the ceiling. A paced-down line item never drops
// to zero probability, because a line item that stops entirely cannot discover
// that conditions changed -- it has no observations, so it cannot recover.
// ---------------------------------------------------------------------------

const (
	PacingEven = "even" // spread across the window (default)
	PacingASAP = "asap" // spend as fast as eligible demand allows

	// minServeProbability keeps a throttled line item present in the auction.
	// Zero probability means no observations, and no observations means no way
	// to notice that the situation changed.
	minServeProbability = 0.05

	// burstFraction is the head start a line item gets at the top of the
	// window. Without it, elapsed is ~0 at midnight, target is ~0, and every
	// line item is throttled to the floor for the first minutes of every day --
	// which looks exactly like a bug and is a common one.
	burstFraction = 0.02
)

// PacingDecision is what the pacer concluded, kept whole so the Decision Trace
// can explain a throttle with numbers rather than a category.
type PacingDecision struct {
	Mode        string  `json:"mode"`
	Elapsed     float64 `json:"elapsed"`      // 0..1 through the window
	TargetSpend float64 `json:"target_spend"` // what we should have spent by now
	ActualSpend float64 `json:"actual_spend"`
	Probability float64 `json:"serve_probability"`
	Throttled   bool    `json:"throttled"`
}

// elapsedToday is the fraction of the UTC day that has passed. The window is a
// day because budgets here are daily; a flight-level pacer would use the flight.
func elapsedToday(now time.Time) float64 {
	u := now.UTC()
	secs := float64(u.Hour()*3600 + u.Minute()*60 + u.Second())
	return secs / 86400.0
}

// Pace decides whether a line item may serve this request.
//
// Returns the decision and whether to serve. Deterministic given rng, so the
// behaviour is testable rather than merely plausible.
func Pace(li *LineItem, spend float64, now time.Time, rng *rand.Rand) (PacingDecision, bool) {
	mode := li.Pacing
	if mode == "" {
		mode = PacingEven
	}
	d := PacingDecision{Mode: mode, ActualSpend: spend, Probability: 1}

	// ASAP and uncapped budgets are not paced. Budget exhaustion is a separate
	// check and stays that way -- conflating "slow down" with "stop" is how a
	// pacer ends up silently enforcing budgets it was never asked to enforce.
	if mode == PacingASAP || li.DailyBudget <= 0 {
		return d, true
	}

	d.Elapsed = elapsedToday(now)
	d.TargetSpend = li.DailyBudget * (d.Elapsed + burstFraction)
	if d.TargetSpend > li.DailyBudget {
		d.TargetSpend = li.DailyBudget
	}

	if spend <= d.TargetSpend || d.TargetSpend <= 0 {
		return d, true
	}

	// Ahead of schedule. Serve with probability target/actual, so being twice
	// ahead halves the rate -- proportional rather than binary.
	p := d.TargetSpend / spend
	if p < minServeProbability {
		p = minServeProbability
	}
	d.Probability = p
	d.Throttled = true

	if rng == nil {
		return d, true // no source of randomness: fail towards delivering
	}
	return d, rng.Float64() < p
}

// DeliveryProjection answers the question a pacer makes possible and nobody
// asks: will this line item actually spend its budget today?
//
// This is where pacing meets frequency capping. A cap makes delivery harder
// (fewer eligible opportunities per person) and pacing makes it slower. Both
// are individually reasonable and together they can make a budget
// undeliverable -- and nothing in an ad server says so unless something
// measures it.
type DeliveryProjection struct {
	LineItemID  string  `json:"line_item_id"`
	Elapsed     float64 `json:"elapsed"`
	Spend       float64 `json:"spend"`
	DailyBudget float64 `json:"daily_budget"`
	Projected   float64 `json:"projected_spend"` // at the current rate
	Shortfall   float64 `json:"shortfall"`       // budget - projected, if positive
	OnTrack     bool    `json:"on_track"`
}

// Project extrapolates today's spend rate to the end of the day.
//
// Deliberately naive -- a linear extrapolation, not a model. Traffic is not
// uniform across the day, so this is wrong in a known direction and is still
// enough to answer "is this line item going to miss by a lot?", which is the
// only question anyone acts on.
func Project(li *LineItem, spend float64, now time.Time) DeliveryProjection {
	p := DeliveryProjection{
		LineItemID: li.ID, Spend: spend, DailyBudget: li.DailyBudget,
		Elapsed: elapsedToday(now), OnTrack: true,
	}
	if li.DailyBudget <= 0 {
		return p
	}
	// Too early to extrapolate: a few minutes of data projected across a day
	// produces confident nonsense in both directions.
	if p.Elapsed < 0.05 {
		p.Projected = math.NaN()
		return p
	}
	p.Projected = spend / p.Elapsed
	if p.Projected > li.DailyBudget {
		p.Projected = li.DailyBudget // pacing will hold it at the budget
	}
	if short := li.DailyBudget - p.Projected; short > li.DailyBudget*0.05 {
		p.Shortfall = short
		p.OnTrack = false
	}
	return p
}
