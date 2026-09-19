package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func fieldExp(field, controlMode, treatMode string) Experiment {
	return Experiment{
		Key: FieldExpKey(field), Active: true, Unit: UnitPlacement, ControlID: "ctrl",
		Variants: []Variant{
			{ID: "ctrl", Params: map[string]string{"mode": controlMode}, BucketStart: 0, BucketEnd: 5000},
			{ID: "t", Params: map[string]string{"mode": treatMode}, BucketStart: 5000, BucketEnd: 10000},
		},
	}
}

// A field with no experiment is SENT. The experiment layer exists to test
// removing things we already send; a missing experiment must never silently
// strip a field a buyer depends on.
func TestFieldWithNoExperimentIsSent(t *testing.T) {
	fs, assigns := ResolveFields(Experiments{}, AdRequest{PlacementID: "p1"}, "r1")
	for _, f := range optionalFields {
		if !fs[f.Name] {
			t.Errorf("%s was omitted with no experiment defined", f.Name)
		}
	}
	if len(assigns) != 0 {
		t.Errorf("no experiments defined, but %d assignments were logged", len(assigns))
	}
}

func TestOmitModeRemovesTheField(t *testing.T) {
	e := Experiments{Set: []Experiment{fieldExp("user_eids", "send", "omit")}}
	var sent, omitted int
	for i := 0; i < 400; i++ {
		fs, _ := ResolveFields(e, AdRequest{PlacementID: fmt.Sprintf("p-%d", i)}, "r")
		if fs["user_eids"] {
			sent++
		} else {
			omitted++
		}
	}
	if sent == 0 || omitted == 0 {
		t.Fatalf("both arms should occur: sent=%d omitted=%d", sent, omitted)
	}
	// Other fields are untouched by this experiment.
	fs, _ := ResolveFields(e, AdRequest{PlacementID: "p1"}, "r")
	if !fs["device_ua"] {
		t.Error("an unrelated field was dropped")
	}
}

// Failing towards SENDING is deliberate: an omitted field can silently cost bid
// rate for days, while a redundant one costs bytes.
func TestMalformedModeStillSendsTheField(t *testing.T) {
	for _, bad := range []string{"", "OMIT", "drop", "false", "no"} {
		e := Experiments{Set: []Experiment{fieldExp("device_ua", "send", bad)}}
		for i := 0; i < 50; i++ {
			fs, _ := ResolveFields(e, AdRequest{PlacementID: fmt.Sprintf("p-%d", i)}, "r")
			if !fs["device_ua"] {
				t.Fatalf("mode %q dropped the field; only exactly \"omit\" may drop", bad)
			}
		}
	}
}

// The guarantee that matters most.
func TestProtectedFieldsCannotBeExperimentedOn(t *testing.T) {
	for field := range protectedFields {
		e := Experiments{Set: []Experiment{fieldExp(field, "send", "omit")}}
		problems := e.ValidateFieldExperiments()
		if len(problems) == 0 {
			t.Fatalf("an experiment on protected field %q was accepted", field)
		}
		joined := strings.Join(problems, " ")
		if !strings.Contains(joined, "REFUSED") || !strings.Contains(joined, "protected") {
			t.Errorf("%s: unhelpful refusal: %s", field, joined)
		}
	}
}

// Even if validation were bypassed, a protected field has no representation in
// the field set at all -- there is no code path that omits one.
func TestProtectedFieldsAreNotInTheExperimentableSurface(t *testing.T) {
	for _, f := range optionalFields {
		if why, protected := IsProtected(f.Name); protected {
			t.Fatalf("%s is both optional and protected (%s)", f.Name, why)
		}
	}
	e := Experiments{Set: []Experiment{fieldExp("source_schain", "send", "omit")}}
	fs, _ := ResolveFields(e, AdRequest{PlacementID: "p1"}, "r")
	if _, present := fs["source_schain"]; present {
		t.Fatal("schain appeared in the field set, so something could omit it")
	}
}

func TestIsProtectedAcceptsBareNameAndExperimentKey(t *testing.T) {
	if _, ok := IsProtected("source_schain"); !ok {
		t.Error("bare name not recognised")
	}
	if _, ok := IsProtected(FieldExpKey("source_schain")); !ok {
		t.Error("experiment key not recognised")
	}
	if _, ok := IsProtected("device_ua"); ok {
		t.Error("an ordinary field was reported as protected")
	}
}

func TestUnknownFieldExperimentIsRefused(t *testing.T) {
	e := Experiments{Set: []Experiment{fieldExp("no_such_field", "send", "omit")}}
	problems := e.ValidateFieldExperiments()
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, " "), "not in optionalFields") {
		t.Fatalf("an experiment on an unknown field was accepted: %v", problems)
	}
}

func TestValidFieldExperimentPasses(t *testing.T) {
	e := Experiments{Set: []Experiment{fieldExp("user_eids", "send", "omit")}}
	if p := e.ValidateFieldExperiments(); len(p) != 0 {
		t.Fatalf("a sound field experiment was refused: %v", p)
	}
}

// The end-to-end claim: omitting a field a buyer depends on costs bid rate, and
// the effect is large enough to be measured. This is what makes the experiment
// worth running -- a real DSP never tells you this.
func TestOmittingANeededFieldCostsBidRate(t *testing.T) {
	cp := &ControlPlane{
		Placements: []Placement{{ID: "p1", SiteID: "s1", Width: 300, Height: 250}},
		Creatives:  []Creative{{ID: "cr1", Width: 300, Height: 250}},
		Buyers: []Buyer{{
			ID: "picky", Name: "picky", BaseCPM: 5.0, Variance: 0.1,
			BidRate: 1.0, TimeoutRate: 0, LatencyMS: 10,
			CreativeIDs:      []string{"cr1"},
			FieldSensitivity: map[string]float64{"user_eids": 0.6},
		}},
	}
	run := func(fs FieldSet) int {
		rng := rand.New(rand.NewSource(42))
		bids := 0
		for i := 0; i < 2000; i++ {
			r := AdRequest{PlacementID: "p1", DeviceType: "mobile", Country: "IL"}
			res := runAuction(auctionInput{
				buyers: cp.Buyers, req: &r, pl: &cp.Placements[0], floor: 0,
				tmax: 200, fields: fs, budget: NewMemoryBudget(), cp: cp,
				rng: rng, now: time.Now(),
			})
			bids += res.BidsReceived
		}
		return bids
	}

	with := run(FieldSet{"user_eids": true})
	without := run(FieldSet{"user_eids": false})

	if with == 0 {
		t.Fatal("buyer never bid even with the field present")
	}
	drop := 1 - float64(without)/float64(with)
	if drop < 0.5 || drop > 0.7 {
		t.Fatalf("omitting user_eids cost %.1f%% of bid rate, want ~60%%", drop*100)
	}
	t.Logf("discoverable effect: omitting user.eids cost %.1f%% of this buyer's bid rate", drop*100)
}

// A buyer that does not care about a field is unaffected by omitting it --
// otherwise every experiment would look like it mattered.
func TestOmittingAnIrrelevantFieldCostsNothing(t *testing.T) {
	cp := &ControlPlane{
		Placements: []Placement{{ID: "p1", SiteID: "s1", Width: 300, Height: 250}},
		Creatives:  []Creative{{ID: "cr1", Width: 300, Height: 250}},
		Buyers: []Buyer{{
			ID: "relaxed", BaseCPM: 5.0, Variance: 0.1, BidRate: 0.8,
			LatencyMS: 10, CreativeIDs: []string{"cr1"},
			FieldSensitivity: map[string]float64{"user_eids": 0.6},
		}},
	}
	run := func(fs FieldSet) int {
		rng := rand.New(rand.NewSource(7))
		bids := 0
		for i := 0; i < 2000; i++ {
			r := AdRequest{PlacementID: "p1", DeviceType: "mobile", Country: "IL"}
			res := runAuction(auctionInput{
				buyers: cp.Buyers, req: &r, pl: &cp.Placements[0], floor: 0,
				tmax: 200, fields: fs, budget: NewMemoryBudget(), cp: cp,
				rng: rng, now: time.Now(),
			})
			bids += res.BidsReceived
		}
		return bids
	}
	// Omitting a field this buyer has no sensitivity to.
	with := run(FieldSet{"user_eids": true, "site_keywords": true})
	without := run(FieldSet{"user_eids": true, "site_keywords": false})
	diff := float64(with-without) / float64(with)
	if diff < -0.05 || diff > 0.05 {
		t.Fatalf("omitting an irrelevant field moved bid rate by %.1f%%", diff*100)
	}
}
