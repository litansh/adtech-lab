package main

import (
	"os"
	"strings"
	"testing"
)

func land() Landscape {
	l, err := LoadLandscape("../../product/landscape.json")
	if err != nil {
		panic(err)
	}
	return l
}

// The corpus is edited by an agent on a schedule, so it is exactly the kind of
// file that rots without anyone noticing.
func TestTheRealCorpusIsUsable(t *testing.T) {
	l := land()
	if l.AsOf == "" || len(l.Sources) == 0 || len(l.Portfolio) == 0 {
		t.Fatal("the corpus must carry a date, its sources and our current portfolio")
	}
	for _, c := range l.Candidates {
		if c.BuildDays <= 0 {
			t.Errorf("%s: build_days must be positive, or the score divides by zero", c.ID)
		}
		if c.Evidence == "" {
			t.Errorf("%s: a candidate with no evidence is an opinion with a score attached", c.ID)
		}
		if c.Retention > 5 || c.Fit > 5 || c.Saturation > 5 {
			t.Errorf("%s: the 0-5 scales are the whole comparability of the ranking", c.ID)
		}
	}
}

// The point of the gap analysis: six games that are all deduction and
// score-attack is a narrow portfolio wearing six hats.
func TestGapAnalysisFindsMechanicsWeDoNotShip(t *testing.T) {
	missing := MissingMechanics(land())
	found := strings.Join(missing, ",")
	for _, want := range []string{"categorisation", "word"} {
		if !strings.Contains(found, want) {
			t.Errorf("we ship no %s game; the gap analysis missed it (got %v)", want, missing)
		}
	}
	if strings.Contains(found, "meta") {
		t.Error("'meta' is not a game mechanic and must never be reported as a gap")
	}
	if strings.Contains(found, "adversarial") {
		t.Error("we already ship two adversarial games")
	}
}

// A recurring content obligation must not be averaged into a one-off cost.
// It is how a two-person team ends up owing 365 puzzles a year.
func TestContentObligationIsReportedSeparatelyFromScore(t *testing.T) {
	var daily Candidate
	for _, c := range land().Candidates {
		if c.ID == "grouping-daily" {
			daily = c
		}
	}
	if daily.Obligation() == "" {
		t.Fatal("a 365-sets-a-year game must declare what it owes")
	}
	cheap := daily
	cheap.ContentSetsPerYear = 0
	if cheap.Score() != daily.Score() {
		t.Fatal("content obligation must not move the score — it is a different kind of cost")
	}
}

// A benchmark that only ever says yes is a wish list.
func TestSomethingIsRejected(t *testing.T) {
	out := Benchmark(land())
	if !strings.Contains(out, "SKIP") {
		t.Fatal("nothing was rejected")
	}
	// The $100/month ceiling makes a persistent multiplayer server impossible,
	// however popular the category is. Popularity must not outvote feasibility.
	var io Candidate
	for _, c := range land().Candidates {
		if c.ID == "io-multiplayer" {
			io = c
		}
	}
	if io.Score() >= buildableThreshold {
		t.Fatalf("io-multiplayer scored %.2f — fit must dominate popularity", io.Score())
	}
}

func TestRankingIsDeterministic(t *testing.T) {
	a, b := Ranked(land()), Ranked(land())
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatal("two runs disagreed")
		}
	}
}

// Value-per-day has a known bias: a one-day tweak always outranks a three-day
// platform change. A roadmap of nothing but one-day tweaks never becomes a
// better product, so the report must also name a direction.
func TestReportNamesADirectionNotJustTheCheapestWin(t *testing.T) {
	out := Benchmark(land())
	if !strings.Contains(out, "DIRECT.") {
		t.Fatal("no strategic pick — the report is a list of tweaks")
	}
	i, j := strings.Index(out, "DO NEXT"), strings.Index(out, "DIRECT.")
	if i < 0 || j < i {
		t.Fatal("the cheapest win should come first, the direction second")
	}
	// The direction must be something that changes what the site is.
	line := out[j : j+120]
	if strings.Contains(line, "Near-miss") {
		t.Fatal("a one-day feature was reported as the strategic direction")
	}
}

// --- findings becoming work -------------------------------------------------

// The benchmark ran daily in three workflows for days, printed a ranked answer,
// sent it to a phone, and stopped. Nothing opened an issue, nothing reached the
// backlog. An agent whose findings never become tracked work is a very
// well-tested opinion generator.
func TestTheRecommendationIsRaisedOnceAndThenGoesQuiet(t *testing.T) {
	l := land()
	next, direction, ok := Recommend(l)
	if !ok {
		t.Fatal("nothing recommended")
	}

	if !Changed(Proposal{}, next, direction) {
		t.Fatal("the first run must raise something")
	}
	raised := Proposal{Next: next.ID, Direction: direction.ID, RaisedOn: "2026-08-30"}
	if Changed(raised, next, direction) {
		t.Fatal("an unchanged recommendation must not be raised again — that is how an agent gets muted")
	}
	if !Changed(Proposal{Next: "something-else", Direction: direction.ID}, next, direction) {
		t.Fatal("a changed recommendation must be raised")
	}
}

// Two picks, and they must be different kinds of thing: the cheapest win and a
// direction. A "direction" that is a one-day tweak is not a direction.
func TestTheDirectionChangesWhatTheSiteIs(t *testing.T) {
	_, direction, _ := Recommend(land())
	if direction.ID == "" {
		t.Fatal("no direction picked")
	}
	if direction.Kind != "game" && direction.Kind != "platform" {
		t.Fatalf("the direction is a %q; it must change what the site is", direction.Kind)
	}
}

// The body leads with evidence, and says what the agent has NOT earned. A
// proposal from something with no track record should say so.
func TestTheIssueBodyCarriesEvidenceAndItsOwnCaveat(t *testing.T) {
	l := land()
	next, direction, _ := Recommend(l)
	body := IssueBody(l, next, direction)

	for _, want := range []string{next.Evidence, "never run in shadow mode", "Nothing here is applied", "Gap"} {
		if !strings.Contains(body, want) {
			t.Errorf("the issue body is missing %q", want)
		}
	}
	// The evidence must appear before the ranking table: the score is this
	// tool's opinion, the evidence is not.
	if strings.Index(body, next.Evidence) > strings.Index(body, "The full ranking") {
		t.Error("the ranking came before the evidence")
	}
}

func TestGapReadsAsEnglish(t *testing.T) {
	if got := join([]string{"a", "b", "c"}); got != "a, no b and no c" {
		t.Fatalf("got %q", got)
	}
	if got := join([]string{"a"}); got != "a" {
		t.Fatalf("got %q", got)
	}
}

// The record means "an issue exists for this recommendation", and only the step
// that creates the issue knows whether that is true.
//
// The first version wrote it inside this tool, before the create. So the run
// that failed to raise the finding recorded it as raised anyway — and
// suppressed it permanently, because Changed() then returned false forever. The
// finding was lost by the very run that lost it.
func TestTheToolNeverRecordsThatSomethingWasRaised(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "SaveProposal(") {
		t.Fatal("main.go writes the proposal record. Only the step that creates " +
			"the issue may do that — see .github/workflows/fleet.yml")
	}
}

// And the record has to be believed once it exists, or the agent files the same
// issue nightly.
func TestARaisedRecordSuppressesTheSameRecommendation(t *testing.T) {
	next, direction, _ := Recommend(land())
	raised := Proposal{Next: next.ID, Direction: direction.ID, RaisedOn: "2026-08-30"}
	if Changed(raised, next, direction) {
		t.Fatal("an already-raised recommendation was raised again")
	}
}

// A benchmark that does not know what exists keeps recommending finished work.
// This one put `one-away` at the top of its ranking two days after `one-away`
// shipped, and would have gone on doing so forever -- which makes the
// once-only rule worthless, because the recommendation never changes.
func TestShippedCandidatesLeaveTheRanking(t *testing.T) {
	l := land()

	built := Built(l)
	if len(built) == 0 {
		t.Fatal("nothing is marked shipped; the landscape has lost its history")
	}
	for _, c := range built {
		if c.ShippedIn == "" {
			t.Errorf("%s says it shipped but not where — the record is half a record", c.ID)
		}
	}

	shipped := map[string]bool{}
	for _, c := range built {
		shipped[c.ID] = true
	}
	for _, c := range Ranked(l) {
		if shipped[c.ID] {
			t.Fatalf("%s is built and still being recommended", c.ID)
		}
	}

	next, direction, ok := Recommend(l)
	if !ok {
		t.Fatal("nothing recommended")
	}
	if shipped[next.ID] || shipped[direction.ID] {
		t.Fatalf("recommended something already built: %s / %s", next.ID, direction.ID)
	}
}

// Shown, not silently dropped. A candidate that vanishes from the table looks
// like an oversight; one listed as done is a record.
func TestWhatWasBuiltIsStillReported(t *testing.T) {
	out := Benchmark(land())
	if !strings.Contains(out, "BUILT") {
		t.Fatal("the report does not say what has been built")
	}
	for _, id := range []string{"one-away", "daily-hub"} {
		if !strings.Contains(out, id) {
			t.Errorf("%s disappeared from the report entirely", id)
		}
	}
}

// The agent proposed "streaks" when the daily already had one and Snake
// deliberately did not — game-room.js says in a comment that the site is not
// built on loss aversion. The landscape had nowhere to record a decision, so a
// candidate could be re-proposed against a choice already made and written
// down somewhere the agent cannot read.
func TestDecisionsAlreadyTakenAreCarriedWithTheCandidate(t *testing.T) {
	l := land()
	var withConstraint int
	for _, c := range l.Candidates {
		if c.Constraint == "" {
			continue
		}
		withConstraint++
		if len(c.Constraint) < 20 {
			t.Errorf("%s: a constraint has to say WHY, not just that there is one", c.ID)
		}
	}
	if withConstraint == 0 {
		t.Fatal("no candidate records a decision; the landscape has no memory again")
	}

	// And it has to reach the report, or it is a comment in a JSON file.
	out := Benchmark(l)
	if !strings.Contains(out, "loss aversion") {
		t.Fatal("the Snake decision is not in the report")
	}
}

// A constraint is context, not a veto. Rejection is still the score's job.
func TestAConstraintDoesNotChangeTheScore(t *testing.T) {
	c := Candidate{Retention: 4, Fit: 5, Saturation: 2, BuildDays: 1}
	before := c.Score()
	c.Constraint = "we decided against half of this a month ago"
	if c.Score() != before {
		t.Fatal("a constraint moved the score; it is context, and the score is the judgement")
	}
}
