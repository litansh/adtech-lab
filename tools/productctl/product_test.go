package main

import (
	"math/rand"
	"testing"
)

func arm(name string, sessions, ends, replays int, rev float64) ArmStats {
	return ArmStats{Arm: name, Sessions: sessions, Ends: ends, Replays: replays, Revenue: rev}
}

// The rule the whole agent exists to enforce.
func TestAPlacementThatHurtsReplayIsRemoved(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	on := arm("on", 2000, 2000, 600, 4.0)   // 30% replay, more revenue
	off := arm("off", 2000, 2000, 800, 3.0) // 40% replay
	v := JudgePlacement(on, off, rng)

	if v.Decision != "REMOVE" {
		t.Fatalf("a placement costing a quarter of replay rate got %q (%s)", v.Decision, v.Why)
	}
	if v.RevenueDelta <= 0 {
		t.Error("the test is not exercising the trade unless revenue improved")
	}
}

// The case the asymmetry exists for: revenue up, replay down, inconclusive.
// Shipping here is a bet that the invisible cost is zero.
func TestInconclusiveIsNotAPass(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	on := arm("on", 1000, 1000, 386, 4.0)   // 38.6%
	off := arm("off", 1000, 1000, 400, 3.0) // 40.0% -- slightly better, overlapping
	v := JudgePlacement(on, off, rng)

	if v.Decision == "SHIP" {
		t.Fatalf("shipped on an inconclusive result with replay down: %+v", v)
	}
	if v.ReplayCILow > 0 || v.ReplayCIHigh < 0 {
		t.Fatalf("this case should straddle zero: [%.3f, %.3f]", v.ReplayCILow, v.ReplayCIHigh)
	}
}

func TestAHarmlessPlacementShips(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	on := arm("on", 2000, 2000, 810, 4.0)
	off := arm("off", 2000, 2000, 800, 3.0)
	v := JudgePlacement(on, off, rng)
	if v.Decision != "SHIP" {
		t.Fatalf("a placement that did not hurt replay got %q (%s)", v.Decision, v.Why)
	}
}

// Too little data is not a verdict.
func TestTooFewCompletionsMeansKeepMeasuring(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	v := JudgePlacement(arm("on", 100, 90, 20, 1), arm("off", 100, 95, 40, 0.5), rng)
	if v.Decision != "KEEP MEASURING" {
		t.Fatalf("judged on 90 completions: %q", v.Decision)
	}
}

// Revenue must never be able to buy a verdict. This is the whole point of the
// agent sitting above Yield in the arbitration order.
func TestRevenueCannotOverrideTheReplayVerdict(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	// Ten times the revenue, and clearly worse replay.
	on := arm("on", 2000, 2000, 500, 40.0)
	off := arm("off", 2000, 2000, 800, 4.0)
	v := JudgePlacement(on, off, rng)
	if v.Decision == "SHIP" {
		t.Fatalf("10x revenue bought a SHIP despite replay falling %.0f%%",
			-v.ReplayDelta*100)
	}
}

// --- engagement reporting ---

func TestReportRanksByReplayRate(t *testing.T) {
	sess := func(n int) map[string]bool {
		m := map[string]bool{}
		for i := 0; i < n; i++ {
			m[string(rune('a'+i%26))+string(rune('0'+i/26))] = true
		}
		return m
	}
	rs := Report([]GameStats{
		{Game: "dull", Starts: 500, Ends: 400, Replays: 40, Sessions: sess(300)},
		{Game: "sticky", Starts: 500, Ends: 400, Replays: 240, Sessions: sess(300)},
	})
	if rs[0].Game != "sticky" {
		t.Fatalf("ranked %q first, want the game people replay", rs[0].Game)
	}
	if rs[0].ReplayRate <= rs[1].ReplayRate {
		t.Error("replay rates are not ordered")
	}
}

// A flag has to say what it means, or nobody acts on it.
func TestFlagsExplainRatherThanJustWarn(t *testing.T) {
	rs := Report([]GameStats{{
		Game: "grind", Starts: 1000, Ends: 300, Replays: 15,
		Sessions: map[string]bool{"a": true},
	}})
	fl := Flags(rs)
	if len(fl) == 0 {
		t.Fatal("a game with 30% completion and 5% replay produced no flags")
	}
	joined := ""
	for _, f := range fl {
		joined += f + " | "
	}
	if !contains(joined, "not compelling") || !contains(joined, "cannot tell which") {
		t.Errorf("flags do not explain what they mean: %s", joined)
	}
}

// Quiet when there is nothing to say, and silent on small samples.
func TestNoFlagsOnHealthyOrTinyData(t *testing.T) {
	healthy := Report([]GameStats{{
		Game: "good", Starts: 1000, Ends: 900, Replays: 500,
		Sessions: map[string]bool{"a": true, "b": true},
	}})
	if f := Flags(healthy); len(f) != 0 {
		t.Errorf("healthy game flagged: %v", f)
	}
	tiny := Report([]GameStats{{Game: "new", Starts: 20, Ends: 3, Replays: 0,
		Sessions: map[string]bool{"a": true}}})
	if f := Flags(tiny); len(f) != 0 {
		t.Errorf("judged a game on 20 starts: %v", f)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
