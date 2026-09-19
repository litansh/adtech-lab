package main

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// obsFor builds n sessions whose revenue is drawn from a HEAVY-TAILED
// distribution: most sessions earn nothing and a few earn a lot. That shape is
// the whole reason the controller bootstraps instead of running a t-test.
func obsFor(arm string, n int, meanRev float64, replayRate float64, rng *rand.Rand) []Observation {
	out := make([]Observation, 0, n)
	for i := 0; i < n; i++ {
		rev := 0.0
		paid := 0
		if rng.Float64() < 0.25 { // only a quarter of sessions monetise at all
			rev = rng.ExpFloat64() * meanRev * 4
			paid = 1
		}
		out = append(out, Observation{
			Unit: fmt.Sprintf("s-%d|%s", i, arm), Revenue: rev,
			Requests: 1, Paid: paid, Replayed: rng.Float64() < replayRate,
		})
	}
	return out
}

func TestBootstrapFindsNoDifferenceBetweenIdenticalArms(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	a := obsFor("a", 4000, 0.005, 0.4, rng)
	b := obsFor("b", 4000, 0.005, 0.4, rng)
	iv := BootstrapDiff(a, b, 2000, rng)
	if iv.Significant {
		t.Fatalf("declared a difference between identical arms: %+v", iv)
	}
	if iv.Low > 0 || iv.High < 0 {
		t.Errorf("interval should straddle zero: [%.4f, %.4f]", iv.Low, iv.High)
	}
}

func TestBootstrapFindsALargeRealDifference(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	control := obsFor("ctrl", 6000, 0.004, 0.4, rng)
	better := obsFor("hi", 6000, 0.012, 0.4, rng)
	iv := BootstrapDiff(better, control, 2000, rng)
	if !iv.Significant || iv.Diff <= 0 {
		t.Fatalf("missed a 3x revenue difference: %+v", iv)
	}
}

func TestSummariseUsesPaidFillNotTotalFill(t *testing.T) {
	// The floor sweep's central finding: total fill stayed 100% at every floor
	// because the house fallback always fills. Only paid fill carries signal.
	obs := []Observation{
		{Unit: "a|x", Requests: 10, Paid: 2, Revenue: 0.05},
		{Unit: "b|x", Requests: 10, Paid: 0, Revenue: 0},
	}
	s := Summarise("x", obs)
	if math.Abs(s.PaidFill-0.10) > 1e-9 {
		t.Fatalf("paid fill = %.3f, want 0.10", s.PaidFill)
	}
	if math.Abs(s.RevenuePer1k-2.5) > 1e-9 {
		t.Fatalf("rev/1k = %.4f, want 2.5", s.RevenuePer1k)
	}
}

// The product guardrail has teeth: revenue that costs replays has not won.
func TestReplayRegressionDisqualifiesAWinningArm(t *testing.T) {
	g := DefaultGuardrails()
	control := ArmStats{ID: "ctrl", Units: 5000, PaidFill: 0.6, ReplayRate: 0.40}
	greedy := ArmStats{ID: "hi", Units: 5000, PaidFill: 0.6, ReplayRate: 0.34} // -15%

	b := g.Breaches(greedy, control)
	if !containsStr(b, "replay_rate_regression") {
		t.Fatalf("a 15%% replay drop was not flagged: %v", b)
	}
	// And an arm that leaves replays alone must not be flagged.
	fine := ArmStats{ID: "ok", Units: 5000, PaidFill: 0.6, ReplayRate: 0.39}
	if b := g.Breaches(fine, control); len(b) > 0 {
		t.Fatalf("a healthy arm was flagged: %v", b)
	}
}

func TestGuardrailsRefuseToDecideOnTooLittleData(t *testing.T) {
	g := DefaultGuardrails()
	control := ArmStats{ID: "ctrl", Units: 5000, PaidFill: 0.6, ReplayRate: 0.4}
	thin := ArmStats{ID: "thin", Units: 40, PaidFill: 0.6, ReplayRate: 0.4}
	if !containsStr(g.Breaches(thin, control), "insufficient_volume") {
		t.Fatal("40 sessions was accepted as a result")
	}
}

func TestPaidFillFloorIsEnforced(t *testing.T) {
	g := DefaultGuardrails()
	control := ArmStats{ID: "ctrl", Units: 5000, PaidFill: 0.9, ReplayRate: 0.4}
	// The $11 floor from the sweep: 4.5% paid fill, revenue down 91%.
	starved := ArmStats{ID: "f1100", Units: 5000, PaidFill: 0.045, ReplayRate: 0.4}
	if !containsStr(g.Breaches(starved, control), "paid_fill_below_floor") {
		t.Fatal("4.5% paid fill passed the guardrail")
	}
}

// The holdout is the point of the whole design: the controller must never be
// able to starve the control arm, however certain it becomes.
func TestControlNeverDropsBelowItsHoldout(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	stats := []ArmStats{
		{ID: "ctrl", Units: 9000, RevenuePer1k: 0.10}, // much worse
		{ID: "win", Units: 9000, RevenuePer1k: 50.00}, // overwhelming winner
	}
	for i := 0; i < 200; i++ {
		props := Allocate(stats, "ctrl", 0.20, nil, nil, rng)
		for _, p := range props {
			if p.ArmID == "ctrl" && p.ToShare < 0.20-1e-9 {
				t.Fatalf("control starved to %.3f, holdout is 0.20", p.ToShare)
			}
		}
	}
}

func TestNoArmIsEverStarvedToZero(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	stats := []ArmStats{
		{ID: "ctrl", Units: 9000, RevenuePer1k: 5.0},
		{ID: "bad", Units: 9000, RevenuePer1k: 0.0}, // earns literally nothing
	}
	props := Allocate(stats, "ctrl", 0.05, nil, nil, rng)
	for _, p := range props {
		if p.ToShare < MinArmShare-1e-9 {
			t.Fatalf("arm %s reduced to %.4f, below the %.2f floor -- it could never recover",
				p.ArmID, p.ToShare, MinArmShare)
		}
	}
}

func TestBreachedArmsAreNotAllocatedTraffic(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	stats := []ArmStats{
		{ID: "ctrl", Units: 9000, RevenuePer1k: 4.0},
		{ID: "greedy", Units: 9000, RevenuePer1k: 99.0}, // best revenue...
	}
	// ...but it broke the product guardrail, so revenue must not save it.
	breached := map[string][]string{"greedy": {"replay_rate_regression"}}
	props := Allocate(stats, "ctrl", 0.05, breached, nil, rng)
	for _, p := range props {
		if p.ArmID == "greedy" && p.ToShare > MinArmShare+1e-9 {
			t.Fatalf("a breached arm got %.3f of traffic", p.ToShare)
		}
	}
}

func TestAllocationAlwaysCoversEveryBucket(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	stats := []ArmStats{
		{ID: "a", Units: 3000, RevenuePer1k: 1},
		{ID: "b", Units: 3000, RevenuePer1k: 7},
		{ID: "c", Units: 3000, RevenuePer1k: 3},
	}
	for i := 0; i < 100; i++ {
		props := Allocate(stats, "a", 0.05, nil, nil, rng)
		cursor := 0
		for _, p := range props {
			if p.ToStart != cursor {
				t.Fatalf("gap or overlap at %s: starts %d, expected %d", p.ArmID, p.ToStart, cursor)
			}
			cursor = p.ToEnd
		}
		if cursor != BucketCount {
			t.Fatalf("buckets cover %d of %d", cursor, BucketCount)
		}
	}
}

// Nothing measurable must fall back to the control rather than to noise.
func TestNoSignalFallsBackToControl(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	stats := []ArmStats{
		{ID: "ctrl", Units: 0, RevenuePer1k: 0},
		{ID: "x", Units: 0, RevenuePer1k: 0},
	}
	props := Allocate(stats, "ctrl", 0.05, nil, nil, rng)
	var ctrlShare float64
	for _, p := range props {
		if p.ArmID == "ctrl" {
			ctrlShare = p.ToShare
		}
	}
	if ctrlShare < 0.5 {
		t.Fatalf("with no signal the control got only %.2f", ctrlShare)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// The stated invariant in ADR 0006: at most 10% of buckets move in one run.
// It has to hold in the FINAL numbers -- an earlier version clamped movement
// and then water-filled the remainder, undoing the clamp.
func TestNoArmMovesMoreThanTheLimitInOneRun(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	stats := []ArmStats{
		{ID: "ctrl", Units: 9000, RevenuePer1k: 1.0},
		{ID: "a", Units: 9000, RevenuePer1k: 90.0}, // overwhelming winner
		{ID: "b", Units: 9000, RevenuePer1k: 0.01},
	}
	current := map[string]float64{"ctrl": 0.20, "a": 0.40, "b": 0.40}
	for i := 0; i < 200; i++ {
		for _, p := range Allocate(stats, "ctrl", 0.20, nil, current, rng) {
			if d := math.Abs(p.ToShare - current[p.ArmID]); d > MaxMovePerRun+1e-9 {
				t.Fatalf("%s moved %.4f in one run, limit is %.2f", p.ArmID, d, MaxMovePerRun)
			}
		}
	}
}

// Repeatedly applying the controller must converge on the winner rather than
// oscillating -- and must still never breach the holdout on the way.
func TestRepeatedRunsConvergeOnTheWinner(t *testing.T) {
	rng := rand.New(rand.NewSource(29))
	stats := []ArmStats{
		{ID: "ctrl", Units: 9000, RevenuePer1k: 1.0},
		{ID: "win", Units: 9000, RevenuePer1k: 9.0},
	}
	current := map[string]float64{"ctrl": 0.50, "win": 0.50}
	for i := 0; i < 40; i++ {
		next := map[string]float64{}
		for _, p := range Allocate(stats, "ctrl", 0.20, nil, current, rng) {
			next[p.ArmID] = p.ToShare
		}
		if next["ctrl"] < 0.20-1e-9 {
			t.Fatalf("run %d breached the holdout: %.4f", i, next["ctrl"])
		}
		current = next
	}
	if current["win"] < 0.70 {
		t.Fatalf("after 40 runs the winner holds only %.2f", current["win"])
	}
	// The holdout is a FLOOR, not a pin: the control also competes for the
	// remainder on merit, so it settles a little above 0.20 rather than exactly
	// on it. What must never happen is settling below.
	if current["ctrl"] < 0.20 || current["ctrl"] > 0.30 {
		t.Errorf("control settled at %.3f, want [0.20, 0.30]", current["ctrl"])
	}
}

// ADR 0006 says a guardrail breach reverts immediately. The move limit exists
// so a noisy hour cannot swing the platform -- a breach is not noise, and
// draining it over several runs costs money on every one of them.
func TestBreachedArmDrainsImmediatelyDespiteTheMoveLimit(t *testing.T) {
	rng := rand.New(rand.NewSource(31))
	stats := []ArmStats{
		{ID: "ctrl", Units: 9000, RevenuePer1k: 4.0},
		{ID: "bad", Units: 9000, RevenuePer1k: 9.0},
	}
	current := map[string]float64{"ctrl": 0.20, "bad": 0.80}
	breached := map[string][]string{"bad": {"paid_fill_below_floor"}}
	for _, p := range Allocate(stats, "ctrl", 0.20, breached, current, rng) {
		if p.ArmID == "bad" && p.ToShare > MinArmShare+1e-9 {
			t.Fatalf("breached arm still holds %.3f after one run", p.ToShare)
		}
	}
}
