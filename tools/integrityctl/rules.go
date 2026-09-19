package main

import (
	"math"
	"sort"
)

// ---------------------------------------------------------------------------
// Layer 4: retroactive billability.
//
// The pre-auction classifier in filter.go sees ONE request and has single-digit
// milliseconds. That is a genuinely weak position, and pretending otherwise is
// how pre-bid filtering acquires false confidence. The strongest evidence
// arrives AFTER the decision:
//
//   - a click that lands before the creative could have rendered
//   - impressions in a session where nobody ever started a game
//   - one "session" appearing from several countries
//   - inter-request intervals with the variance of a metronome
//
// None of it is available pre-bid. All of it is available an hour later, for
// almost nothing. So this tool does not try to be a better classifier -- it
// answers a different question: given everything we know now, was that call
// right, and what should we not have billed for?
//
// Every rule is written to be WRONG IN THE SAFE DIRECTION. A false positive
// here refunds an advertiser who owed us money; a false negative bills an
// advertiser for a bot. Those are not symmetric, and the thresholds lean
// accordingly -- but they are thresholds, not accuracies. There is no ground
// truth in this dataset, so precision and recall are unmeasured and this file
// says so rather than implying otherwise.
// ---------------------------------------------------------------------------

// Impression is one billable event under review.
type Impression struct {
	EventID  string
	TSMillis int64
	Revenue  float64
	Clicked  bool
	ClickTS  int64
}

// SessionView is everything logged about one session, assembled by main.go.
type SessionView struct {
	SessionID   string
	Impressions []Impression
	Countries   []string
	// RequestTS are ad_request timestamps, used for timing regularity.
	RequestTS []int64
	// GameStarts counts game_start events. Zero means nobody played.
	GameStarts int
	Env        string
}

// Revocation is the finding for one session.
type Revocation struct {
	SessionID string
	Reasons   []string
	EventIDs  []string
	Revenue   float64
}

const (
	ReasonClickBeforeRender      = "click_before_render"
	ReasonImpressionsWithoutPlay = "impressions_without_play"
	ReasonImplausibleCTR         = "implausible_ctr"
	ReasonMultipleCountries      = "session_across_countries"
	ReasonMetronomicTiming       = "metronomic_request_timing"
)

// Thresholds. Named constants because every one of them is a judgement call
// that someone will want to argue with, and an argument needs a name.
type Thresholds struct {
	// MinRenderMS is how long after an impression a click could physically be
	// genuine. A human has to see the creative and move a finger.
	MinRenderMS int64
	// MinImpressionsForCTR is the volume below which a high CTR is just a small
	// number. One click on one impression is 100% CTR and means nothing.
	MinImpressionsForCTR int
	MaxPlausibleCTR      float64
	// MinRequestsForTiming and MaxTimingCV: humans are not metronomes. A
	// coefficient of variation near zero across many requests is a scheduler.
	MinRequestsForTiming int
	MaxTimingCV          float64
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		MinRenderMS:          300,
		MinImpressionsForCTR: 4,
		MaxPlausibleCTR:      0.75,
		MinRequestsForTiming: 8,
		MaxTimingCV:          0.02,
	}
}

// Review applies every rule to one session. Returns nil when the session is
// sound, so a caller can treat nil as "nothing to do".
func Review(s SessionView, t Thresholds) *Revocation {
	var reasons []string

	// 1. A click that arrives before the creative could have rendered did not
	// come from a person looking at it.
	for _, im := range s.Impressions {
		if im.Clicked && im.ClickTS-im.TSMillis < t.MinRenderMS {
			reasons = append(reasons, ReasonClickBeforeRender)
			break
		}
	}

	// 2. Ads served into a session where no game was ever started. The visitor
	// loaded the page and never touched it -- which is exactly what a prefetch,
	// a scraper or a headless browser looks like after the fact.
	if len(s.Impressions) > 0 && s.GameStarts == 0 {
		reasons = append(reasons, ReasonImpressionsWithoutPlay)
	}

	// 3. Implausible CTR, but only with enough impressions for the ratio to
	// mean anything.
	if n := len(s.Impressions); n >= t.MinImpressionsForCTR {
		clicks := 0
		for _, im := range s.Impressions {
			if im.Clicked {
				clicks++
			}
		}
		if float64(clicks)/float64(n) > t.MaxPlausibleCTR {
			reasons = append(reasons, ReasonImplausibleCTR)
		}
	}

	// 4. One session, several countries. A person does not change country
	// mid-session; a rotating proxy pool does.
	if distinct(s.Countries) > 1 {
		reasons = append(reasons, ReasonMultipleCountries)
	}

	// 5. Metronomic request timing.
	if cv, ok := intervalCV(s.RequestTS, t.MinRequestsForTiming); ok && cv < t.MaxTimingCV {
		reasons = append(reasons, ReasonMetronomicTiming)
	}

	if len(reasons) == 0 {
		return nil
	}

	rev := &Revocation{SessionID: s.SessionID, Reasons: reasons}
	for _, im := range s.Impressions {
		rev.EventIDs = append(rev.EventIDs, im.EventID)
		rev.Revenue += im.Revenue
	}
	return rev
}

func distinct(xs []string) int {
	seen := map[string]bool{}
	for _, x := range xs {
		if x != "" {
			seen[x] = true
		}
	}
	return len(seen)
}

// intervalCV returns the coefficient of variation of the gaps between
// timestamps. CV rather than raw variance so the measure is scale-free: a bot
// polling every 5 seconds and one polling every 5 minutes are equally regular.
func intervalCV(ts []int64, minCount int) (float64, bool) {
	if len(ts) < minCount {
		return 0, false
	}
	sorted := append([]int64(nil), ts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	gaps := make([]float64, 0, len(sorted)-1)
	for i := 1; i < len(sorted); i++ {
		gaps = append(gaps, float64(sorted[i]-sorted[i-1]))
	}
	var mean float64
	for _, g := range gaps {
		mean += g
	}
	mean /= float64(len(gaps))
	if mean <= 0 {
		// Every request at the same instant is not "regular", it is one batch.
		return 0, false
	}
	var ss float64
	for _, g := range gaps {
		ss += (g - mean) * (g - mean)
	}
	return math.Sqrt(ss/float64(len(gaps))) / mean, true
}
