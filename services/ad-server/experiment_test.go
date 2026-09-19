package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func floorExp() Experiments {
	return Experiments{Set: []Experiment{{
		Key: "floor_price", Active: true, Unit: UnitSession, ControlID: "ctrl",
		Variants: []Variant{
			{ID: "ctrl", Params: map[string]string{"floor": "0.50"}, BucketStart: 0, BucketEnd: 2000},
			{ID: "f1", Params: map[string]string{"floor": "1.00"}, BucketStart: 2000, BucketEnd: 6000},
			{ID: "f2", Params: map[string]string{"floor": "2.00"}, BucketStart: 6000, BucketEnd: 10000},
		},
	}}}
}

func TestAssignIsDeterministic(t *testing.T) {
	e := floorExp()
	r := AdRequest{SessionID: "sess-abc", PlacementID: "p1"}
	first, _ := e.Assign("floor_price", r, "req-1")
	// Same session, different request id: a session-unit experiment must not
	// move the player between arms mid-session.
	for i := 0; i < 50; i++ {
		got, _ := e.Assign("floor_price", r, fmt.Sprintf("req-%d", i))
		if got.ID != first.ID {
			t.Fatalf("session reassigned: %s then %s", first.ID, got.ID)
		}
	}
}

func TestAssignRespectsBucketShares(t *testing.T) {
	e := floorExp()
	counts := map[string]int{}
	const n = 60000
	for i := 0; i < n; i++ {
		r := AdRequest{SessionID: fmt.Sprintf("s-%d", i)}
		v, _ := e.Assign("floor_price", r, "req")
		counts[v.ID]++
	}
	want := map[string]float64{"ctrl": 0.20, "f1": 0.40, "f2": 0.40}
	for id, w := range want {
		got := float64(counts[id]) / n
		if math.Abs(got-w) > 0.01 {
			t.Errorf("variant %s got %.3f of traffic, want %.2f", id, got, w)
		}
	}
}

// The reason for bucket ranges rather than live weights: shifting traffic must
// move only the players at the boundary, not reshuffle everyone.
func TestWeightShiftMovesOnlyBoundaryTraffic(t *testing.T) {
	before := floorExp()
	after := floorExp()
	// Controller moves 10% from f1 to f2.
	after.Set[0].Variants[1].BucketEnd = 5000
	after.Set[0].Variants[2].BucketStart = 5000

	moved, total := 0, 20000
	for i := 0; i < total; i++ {
		r := AdRequest{SessionID: fmt.Sprintf("s-%d", i)}
		a, _ := before.Assign("floor_price", r, "req")
		b, _ := after.Assign("floor_price", r, "req")
		if a.ID != b.ID {
			moved++
		}
	}
	frac := float64(moved) / float64(total)
	if frac > 0.12 {
		t.Fatalf("shifting 10%% of traffic reassigned %.1f%% of sessions; "+
			"bucket ranges are not stable", frac*100)
	}
}

// Two experiments must not put the same session in correlated positions, or
// interaction effects are invisible.
func TestExperimentsAreIndependent(t *testing.T) {
	e := Experiments{Set: []Experiment{
		{Key: "a", Active: true, Unit: UnitSession, ControlID: "c", Variants: []Variant{
			{ID: "c", BucketStart: 0, BucketEnd: 5000}, {ID: "t", BucketStart: 5000, BucketEnd: 10000}}},
		{Key: "b", Active: true, Unit: UnitSession, ControlID: "c", Variants: []Variant{
			{ID: "c", BucketStart: 0, BucketEnd: 5000}, {ID: "t", BucketStart: 5000, BucketEnd: 10000}}},
	}}
	same, n := 0, 20000
	for i := 0; i < n; i++ {
		r := AdRequest{SessionID: fmt.Sprintf("s-%d", i)}
		va, _ := e.Assign("a", r, "req")
		vb, _ := e.Assign("b", r, "req")
		if va.ID == vb.ID {
			same++
		}
	}
	if p := float64(same) / float64(n); math.Abs(p-0.5) > 0.02 {
		t.Fatalf("arms agree %.1f%% of the time, want ~50%%: experiments are correlated", p*100)
	}
}

func TestRequestUnitVariesWithinSession(t *testing.T) {
	e := floorExp()
	e.Set[0].Unit = UnitRequest
	r := AdRequest{SessionID: "one-session"}
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		v, _ := e.Assign("floor_price", r, fmt.Sprintf("req-%d", i))
		seen[v.ID] = true
	}
	if len(seen) < 2 {
		t.Fatal("request-unit experiment never varied within a session")
	}
}

// A bad control-plane edit must degrade to serving normally, never to an error.
func TestBrokenConfigurationServesControl(t *testing.T) {
	cases := map[string]Experiments{
		"unknown key": floorExp(),
		"inactive":    {Set: []Experiment{{Key: "floor_price", Active: false, ControlID: "ctrl", Variants: []Variant{{ID: "ctrl", Params: map[string]string{"floor": "0.50"}, BucketStart: 0, BucketEnd: 10000}}}}},
		"no variants": {Set: []Experiment{{Key: "floor_price", Active: true, ControlID: "ctrl"}}},
		"buckets do not cover": {Set: []Experiment{{Key: "floor_price", Active: true, ControlID: "ctrl", Variants: []Variant{
			{ID: "ctrl", Params: map[string]string{"floor": "0.50"}, BucketStart: 0, BucketEnd: 10},
		}}}},
	}
	for name, e := range cases {
		key := "floor_price"
		if name == "unknown key" {
			key = "not_an_experiment"
		}
		v, a := e.Assign(key, AdRequest{SessionID: "s"}, "r")
		if a.VariantID == "" {
			t.Errorf("%s: empty assignment", name)
		}
		if got := v.Float("floor", 0.50); got != 0.50 {
			t.Errorf("%s: served floor %.2f, want the control 0.50", name, got)
		}
	}
}

func TestValidateCatchesSilentlyBiasedSplits(t *testing.T) {
	tests := []struct {
		name string
		exp  Experiment
		want string
	}{
		{"gap between ranges", Experiment{Key: "k", ControlID: "a", Variants: []Variant{
			{ID: "a", BucketStart: 0, BucketEnd: 4000}, {ID: "b", BucketStart: 5000, BucketEnd: 10000}}},
			"gap or overlap"},
		{"overlapping ranges", Experiment{Key: "k", ControlID: "a", Variants: []Variant{
			{ID: "a", BucketStart: 0, BucketEnd: 6000}, {ID: "b", BucketStart: 5000, BucketEnd: 10000}}},
			"gap or overlap"},
		{"short of full coverage", Experiment{Key: "k", ControlID: "a", Variants: []Variant{
			{ID: "a", BucketStart: 0, BucketEnd: 9000}}},
			"buckets cover 9000"},
		{"holdout too small", Experiment{Key: "k", ControlID: "a", Variants: []Variant{
			{ID: "a", BucketStart: 0, BucketEnd: 200}, {ID: "b", BucketStart: 200, BucketEnd: 10000}}},
			"holdout is 2%"},
		{"control not found", Experiment{Key: "k", ControlID: "missing", Variants: []Variant{
			{ID: "a", BucketStart: 0, BucketEnd: 10000}}},
			"not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			problems := Experiments{Set: []Experiment{tc.exp}}.Validate()
			joined := strings.Join(problems, " | ")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("want a problem containing %q, got: %s", tc.want, joined)
			}
		})
	}
}

func TestValidateAcceptsASoundSet(t *testing.T) {
	if p := floorExp().Validate(); len(p) != 0 {
		t.Fatalf("sound configuration reported problems: %v", p)
	}
}

func TestHoldoutIsFlaggedForAttribution(t *testing.T) {
	e := floorExp()
	var holdout, treated int
	for i := 0; i < 10000; i++ {
		_, a := e.Assign("floor_price", AdRequest{SessionID: fmt.Sprintf("s-%d", i)}, "r")
		if a.Holdout {
			holdout++
		} else {
			treated++
		}
	}
	if holdout == 0 || treated == 0 {
		t.Fatalf("holdout flag never varied: holdout=%d treated=%d", holdout, treated)
	}
}

// The shipped configuration must be sound. A fixture that fails Validate would
// serve control to everyone while looking like a running experiment.
func TestShippedFixtureExperimentsAreValid(t *testing.T) {
	cp, err := loadControlPlane("fixtures/control-plane.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	if len(cp.Experiments) == 0 {
		t.Fatal("no experiments in the shipped control plane")
	}
	if problems := (Experiments{Set: cp.Experiments}).Validate(); len(problems) > 0 {
		t.Fatalf("shipped experiments are invalid:\n  %s", strings.Join(problems, "\n  "))
	}
}
