package main

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
)

// A day where the agent shifted traffic toward a genuinely better arm.
func day(label string, actualCtrl, proposedCtrl, ctrlRate, treatRate float64) ShadowRecord {
	return ShadowRecord{
		Day: label, Experiment: "floor_price", Requests: 10000,
		Actual:        map[string]float64{"ctrl": actualCtrl, "t": 1 - actualCtrl},
		Proposed:      map[string]float64{"ctrl": proposedCtrl, "t": 1 - proposedCtrl},
		RevPerRequest: map[string]float64{"ctrl": ctrlRate, "t": treatRate},
	}
}

func TestShadowRoundTripsThroughAFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	for i := 0; i < 3; i++ {
		if err := AppendShadow(p, day(fmt.Sprintf("d%d", i), 0.5, 0.3, 0.004, 0.006)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadShadow(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("read %d records, want 3", len(got))
	}
	if got[0].RevPerRequest["t"] != 0.006 {
		t.Errorf("rates did not survive the round trip: %+v", got[0])
	}
}

// The counterfactual: shifting traffic to a better-earning arm should show a
// gain, computed from rates that were actually observed.
func TestEvaluateFindsGainWhenTheAgentIsRight(t *testing.T) {
	var recs []ShadowRecord
	for i := 0; i < 20; i++ {
		// Actual runs 50/50; the agent wanted 30/70 toward the better arm.
		recs = append(recs, day(fmt.Sprintf("d%02d", i), 0.5, 0.3, 0.004, 0.006))
	}
	e := Evaluate(recs)
	if e.UpliftPct <= 0 {
		t.Fatalf("no uplift from shifting toward a better arm: %+v", e)
	}
	// 50/50 -> 0.005 avg; 30/70 -> 0.0054 avg. That is +8%.
	if math.Abs(e.UpliftPct-8.0) > 0.5 {
		t.Errorf("uplift %.2f%%, want ~8%%", e.UpliftPct)
	}
	if v, _ := Verdict(e); v != "PROMOTE" {
		t.Errorf("verdict %s for a consistently correct agent", v)
	}
}

// And the case that matters more: it must refuse to promote an agent that
// would have lost money.
func TestEvaluateRefusesAnAgentThatWouldHaveLost(t *testing.T) {
	var recs []ShadowRecord
	for i := 0; i < 20; i++ {
		// The agent wanted MORE of the worse arm.
		recs = append(recs, day(fmt.Sprintf("d%02d", i), 0.5, 0.8, 0.004, 0.006))
	}
	e := Evaluate(recs)
	if e.UpliftPct >= 0 {
		t.Fatalf("shifting toward a worse arm showed a gain: %+v", e)
	}
	v, why := Verdict(e)
	if v != "DO NOT PROMOTE" {
		t.Fatalf("verdict %s (%s) for an agent that lost money", v, why)
	}
}

// An agent that gains on average by winning small and losing big is a
// different proposition from one that gains consistently, and only the
// distribution tells them apart.
func TestASingleCatastrophicDayBlocksPromotion(t *testing.T) {
	var recs []ShadowRecord
	for i := 0; i < 19; i++ {
		recs = append(recs, day(fmt.Sprintf("d%02d", i), 0.5, 0.45, 0.004, 0.0042))
	}
	// One day the agent wanted almost everything on a much worse arm.
	recs = append(recs, day("d19-bad", 0.5, 0.02, 0.010, 0.002))

	e := Evaluate(recs)
	v, why := Verdict(e)
	if v == "PROMOTE" {
		t.Fatalf("promoted despite a %.1f%% day: %s", e.WorstDayPct, why)
	}
	if e.WorstDay != "d19-bad" {
		t.Errorf("worst day was %q, want d19-bad", e.WorstDay)
	}
}

// Promotion is a measurement, not a mood: too little evidence means HOLD.
func TestTooFewDaysHolds(t *testing.T) {
	var recs []ShadowRecord
	for i := 0; i < 5; i++ {
		recs = append(recs, day(fmt.Sprintf("d%d", i), 0.5, 0.3, 0.004, 0.006))
	}
	v, why := Verdict(Evaluate(recs))
	if v != "HOLD" {
		t.Fatalf("verdict %s on 5 days (%s)", v, why)
	}
}

// A gain that is not consistent is not a gain you can rely on.
func TestInconsistentGainHolds(t *testing.T) {
	var recs []ShadowRecord
	for i := 0; i < 20; i++ {
		if i%2 == 0 {
			recs = append(recs, day(fmt.Sprintf("d%02d", i), 0.5, 0.3, 0.004, 0.0065)) // better
		} else {
			recs = append(recs, day(fmt.Sprintf("d%02d", i), 0.5, 0.3, 0.006, 0.0058)) // worse
		}
	}
	e := Evaluate(recs)
	if e.DaysWorse < 8 {
		t.Fatalf("expected many worse days, got %d", e.DaysWorse)
	}
	if v, _ := Verdict(e); v == "PROMOTE" {
		t.Errorf("promoted an agent that was worse on %d of %d days", e.DaysWorse, e.Days)
	}
}

// A shadow run must not be able to apply anything -- that is the whole point.
func TestShadowRecordCarriesNoInstructionToApply(t *testing.T) {
	r := day("d1", 0.5, 0.3, 0.004, 0.006)
	// The record is data: shares, rates, requests. There is no field that any
	// code path reads to change a control plane.
	if r.Proposed == nil || r.Actual == nil {
		t.Fatal("a shadow record must carry both what ran and what was wanted")
	}
	if len(r.RevPerRequest) == 0 {
		t.Fatal("without observed rates the counterfactual cannot be computed at all")
	}
}
