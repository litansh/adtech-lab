package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// ---------------------------------------------------------------------------
// Shadow mode.
//
// The agent computes what it WOULD have done, does not do it, and records the
// decision beside what actually happened. After a few weeks you can answer,
// with data rather than assertion:
//
//     "Would this agent have made us money? By how much? How often would it
//      have been wrong, and how badly?"
//
// Nobody skips Observe, because it is obviously safe. Everybody wants to skip
// Shadow, because the agent already "works" -- and Shadow is the only stage
// that produces evidence a sceptical reader can act on. An agent that cannot
// show a shadow record has not earned the right to act.
//
// THE COUNTERFACTUAL, AND ITS ASSUMPTION
//
// We cannot observe what the proposed allocation would have earned, because it
// was never served. But per-arm revenue RATES are observed -- every arm ran, on
// real traffic, for the whole period. So:
//
//     estimated_revenue(allocation) = Σ share_i × rev_per_request_i × requests
//
// The assumption, stated plainly because it is the thing that can be wrong:
// **an arm's revenue rate does not change with the share of traffic it gets.**
//
// That holds when arms are independent and fails when they interact -- a floor
// that teaches buyers something different at 80% of traffic than at 20%, for
// instance. It is a reasonable assumption for small share changes and a poor
// one for large ones, which is another reason the controller moves at most 10%
// of buckets per run.
// ---------------------------------------------------------------------------

// ShadowRecord is one run's decision, written and never applied.
type ShadowRecord struct {
	Day        string `json:"day"`
	Experiment string `json:"experiment"`
	Requests   int    `json:"requests"`
	// Actual is the share each arm really served.
	Actual map[string]float64 `json:"actual_shares"`
	// Proposed is what the controller would have set.
	Proposed map[string]float64 `json:"proposed_shares"`
	// RevPerRequest per arm, observed. This is what makes the counterfactual
	// computable at all.
	RevPerRequest map[string]float64  `json:"rev_per_request"`
	Breached      map[string][]string `json:"breached,omitempty"`
}

// Evaluation is the answer the whole stage exists to produce.
type Evaluation struct {
	Days        int     `json:"days"`
	ActualUSD   float64 `json:"actual_usd"`
	ProposedUSD float64 `json:"proposed_usd"`
	UpliftUSD   float64 `json:"uplift_usd"`
	UpliftPct   float64 `json:"uplift_pct"`
	DaysBetter  int     `json:"days_better"`
	DaysWorse   int     `json:"days_worse"`
	WorstDayPct float64 `json:"worst_day_pct"`
	WorstDay    string  `json:"worst_day"`
}

func AppendShadow(path string, r ShadowRecord) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func LoadShadow(path string) ([]ShadowRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []ShadowRecord
	for _, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var r ShadowRecord
		if json.Unmarshal(line, &r) != nil {
			continue // one bad line should not cost the whole history
		}
		out = append(out, r)
	}
	return out, nil
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// estimate applies an allocation to observed per-arm rates.
func estimate(shares, rates map[string]float64, requests int) float64 {
	var total float64
	for arm, share := range shares {
		total += share * rates[arm] * float64(requests)
	}
	return total
}

// Evaluate answers whether the agent would have helped.
//
// DaysWorse and WorstDayPct matter as much as the total. An agent that gains 3%
// on average by winning small and losing big is a different proposition from
// one that gains 3% consistently, and only the distribution tells them apart.
func Evaluate(records []ShadowRecord) Evaluation {
	e := Evaluation{Days: len(records)}
	for _, r := range records {
		a := estimate(r.Actual, r.RevPerRequest, r.Requests)
		p := estimate(r.Proposed, r.RevPerRequest, r.Requests)
		e.ActualUSD += a
		e.ProposedUSD += p

		if a > 0 {
			dayPct := (p - a) / a * 100
			switch {
			case p > a:
				e.DaysBetter++
			case p < a:
				e.DaysWorse++
			}
			if dayPct < e.WorstDayPct {
				e.WorstDayPct = dayPct
				e.WorstDay = r.Day
			}
		}
	}
	e.UpliftUSD = e.ProposedUSD - e.ActualUSD
	if e.ActualUSD > 0 {
		e.UpliftPct = e.UpliftUSD / e.ActualUSD * 100
	}
	return e
}

// Verdict turns the evaluation into the promotion decision from
// docs/portable-agents.md, so promotion is a measurement rather than a mood.
func Verdict(e Evaluation) (string, string) {
	switch {
	case e.Days < 14:
		return "HOLD", fmt.Sprintf("only %d days of shadow; promotion needs at least 14", e.Days)
	case e.UpliftPct <= 0:
		return "DO NOT PROMOTE", fmt.Sprintf("the agent would have LOST %.2f%%", -e.UpliftPct)
	case e.WorstDayPct < -10:
		// A single bad day worse than -10% means the agent is capable of real
		// damage, whatever its average says.
		return "DO NOT PROMOTE", fmt.Sprintf(
			"average uplift is +%.2f%% but the worst day was %.2f%% (%s): capable of real damage",
			e.UpliftPct, e.WorstDayPct, e.WorstDay)
	case float64(e.DaysWorse) > float64(e.Days)*0.4:
		return "HOLD", fmt.Sprintf("worse on %d of %d days; the gain is not consistent",
			e.DaysWorse, e.Days)
	default:
		return "PROMOTE", fmt.Sprintf(
			"+%.2f%% over %d days, worse on %d, worst day %.2f%%",
			e.UpliftPct, e.Days, e.DaysWorse, e.WorstDayPct)
	}
}

func sortedArms(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
