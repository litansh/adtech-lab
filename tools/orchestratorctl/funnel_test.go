package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func healthy() World {
	w := base()
	w.LiveGames = 6
	w.ItchViews7d, w.ItchPlays7d = 400, 200
	w.GameViews7d, w.Replays7d = 100, 60
	w.Sessions7d = 80
	w.ReturningKnown, w.Returning7d = true, 0.30
	w.RevenueUSD7d = 0.42
	return w
}

func TestHealthyFunnelRecommendsNothing(t *testing.T) {
	_, verdict, _ := Diagnose(healthy())
	if !strings.Contains(verdict, "healthy") {
		t.Fatalf("got %q", verdict)
	}
}

// The rule that makes the advice worth anything: report the EARLIEST broken
// stage. Here both reach and revenue are broken; recommending the revenue fix
// would send someone to tune an ad stack that 12 people a week will see.
func TestEarliestBrokenStageWins(t *testing.T) {
	w := healthy()
	w.ItchViews7d, w.ItchPlays7d = 12, 6
	w.RevenueUSD7d = 0

	_, verdict, action := Diagnose(w)
	if !strings.HasPrefix(verdict, "reach") {
		t.Fatalf("want reach first, got %q", verdict)
	}
	if strings.Contains(action, "ad stack") {
		t.Fatalf("recommended a downstream fix: %q", action)
	}
}

// Blindness is not health. A funnel that skips the stage it cannot see will
// confidently blame the next one.
func TestBlindStageStopsTheDiagnosis(t *testing.T) {
	w := healthy()
	w.ItchViews7d = -1 // nobody typed this week's number in

	_, verdict, _ := Diagnose(w)
	if !strings.Contains(verdict, "blind at reach") {
		t.Fatalf("want blindness reported, got %q", verdict)
	}
}

func TestReturnStageIsBlindUntilTheFlagExists(t *testing.T) {
	w := healthy()
	w.ReturningKnown = false

	_, verdict, action := Diagnose(w)
	if !strings.Contains(verdict, "blind at return") {
		t.Fatalf("want blindness, got %q", verdict)
	}
	if !strings.Contains(action, "sessionStorage") {
		t.Fatalf("the action must say what to instrument, got %q", action)
	}
}

// Zero traffic must read as "publish something", not as an ad stack problem.
func TestEmptyWorldPointsAtDistribution(t *testing.T) {
	w := base()
	w.LiveGames = 0 // checked, and genuinely nothing published
	_, verdict, action := Diagnose(w)
	if !strings.HasPrefix(verdict, "distribution") {
		t.Fatalf("got %q", verdict)
	}
	if !strings.Contains(action, "GO-LIVE") {
		t.Fatalf("got %q", action)
	}
}

func TestAgentsThatNeedNoTrafficAreRunnableOnDayZero(t *testing.T) {
	runnable, waiting := FleetSchedule(base())
	if len(runnable) == 0 {
		t.Fatal("some agents read the outside world and can always run")
	}
	if len(waiting) == 0 {
		t.Fatal("with no traffic, the data-driven agents must report as waiting")
	}
	for _, a := range runnable {
		if a.NeedsTraffic {
			t.Fatalf("%s needs traffic and there is none", a.Name)
		}
	}
}

// The tool's own failure mode. Offline, itch.io cannot be reached, and an
// earlier version reported "0/6 games" -- turning "I could not look" into
// "you have published nothing", which is the exact confusion this whole
// program was written to stop making.
func TestUnreachableItchIsBlindnessNotFailure(t *testing.T) {
	w := healthy()
	w.LiveGames = -1

	stages, verdict, _ := Diagnose(w)
	if !strings.Contains(verdict, "blind at distribution") {
		t.Fatalf("want blindness, got %q", verdict)
	}
	if strings.Contains(stages[0].Have, "0/6") {
		t.Fatalf("must not render an unknown as a zero: %q", stages[0].Have)
	}
}

// --- does the fleet actually run? ------------------------------------------
//
// Written after a day on which three agents were found silently dead while the
// digest cheerfully reported "4 agents can run today". It was answering whether
// they COULD run; nobody was asking whether they HAD.

func at(hoursAgo int, conclusion string) Run {
	return Run{
		Conclusion: conclusion,
		Status:     "completed",
		CreatedAt:  time.Now().UTC().Add(-time.Duration(hoursAgo) * time.Hour).Format(time.RFC3339),
	}
}

// Asserted as an invariant rather than against a named agent: FinOps was the
// example when this was written, and it was scheduled two hours later, which
// broke the test for the best possible reason. The property is what matters --
// an agent with no workflow must report as unscheduled, and one with a workflow
// must not.
func TestUnscheduledIsExactlyTheAgentsWithNoWorkflow(t *testing.T) {
	hs := CheckFleet(time.Now().UTC(), func(string) (Run, bool, error) { return at(1, "success"), true, nil })
	if len(hs) == 0 {
		t.Fatal("no agents")
	}
	for _, h := range hs {
		hasWorkflow := h.Agent.Workflow != ""
		unscheduled := h.State == HealthUnscheduled
		if hasWorkflow == unscheduled {
			t.Errorf("%s: workflow=%q but state=%q", h.Agent.Name, h.Agent.Workflow, h.State)
		}
	}
}

// The literal shape of today's failure: agent-proposals.yml would not parse, so
// the Yield and Feedback agents had never executed. A green-looking fleet must
// not survive that.
func TestAFailingWorkflowIsLoud(t *testing.T) {
	hs := CheckFleet(time.Now().UTC(), func(w string) (Run, bool, error) {
		if w == "agent-proposals.yml" {
			return at(1, "failure"), true, nil
		}
		return at(1, "success"), true, nil
	})
	var yield Health
	for _, h := range hs {
		if h.Agent.Name == "Yield" {
			yield = h
		}
	}
	if yield.State != HealthFailing || !yield.Loud() {
		t.Fatalf("want a loud failing state, got %q", yield.State)
	}
}

func TestNeverRunIsNotSuccess(t *testing.T) {
	hs := CheckFleet(time.Now().UTC(), func(string) (Run, bool, error) { return Run{}, false, nil })
	for _, h := range hs {
		if h.Agent.Workflow != "" && h.State != HealthNever {
			t.Fatalf("%s: a workflow with no runs is %q, not %q", h.Agent.Name, HealthNever, h.State)
		}
	}
}

// Only at twice the cadence. Anything that reports at the first missed beat
// gets muted, and a muted health check is worse than none.
func TestOneMissedRunIsNotYetOverdue(t *testing.T) {
	look := func(hours int) runLookup {
		return func(string) (Run, bool, error) { return at(hours, "success"), true, nil }
	}
	find := func(hs []Health, name string) Health {
		for _, h := range hs {
			if h.Agent.Name == name {
				return h
			}
		}
		t.Fatalf("no agent %s", name)
		return Health{}
	}
	// Reliability runs every 12h. At 20h one beat has been missed; at 30h two.
	if got := find(CheckFleet(time.Now().UTC(), look(20)), "Reliability"); got.State != HealthOK {
		t.Fatalf("20h with a 12h cadence should still be ok, got %q", got.State)
	}
	if got := find(CheckFleet(time.Now().UTC(), look(30)), "Reliability"); got.State != HealthOverdue {
		t.Fatalf("30h with a 12h cadence is overdue, got %q", got.State)
	}
}

// Unreachable GitHub must not read as a healthy fleet.
func TestNoLookupIsUnknownNotHealthy(t *testing.T) {
	for _, h := range CheckFleet(time.Now().UTC(), nil) {
		if h.State == HealthOK {
			t.Fatalf("%s reported ok without anything being checked", h.Agent.Name)
		}
	}
}

// Every scheduled agent must name a workflow that exists, or the health check
// reports a phantom failure forever.
func TestEveryNamedWorkflowExists(t *testing.T) {
	for _, a := range fleet() {
		if a.Workflow == "" {
			continue
		}
		if _, err := os.Stat("../../.github/workflows/" + a.Workflow); err != nil {
			t.Errorf("%s names %s, which does not exist", a.Name, a.Workflow)
		}
		if a.EveryHours <= 0 {
			t.Errorf("%s has a workflow but no cadence, so it can never be overdue", a.Name)
		}
	}
}

// The health check's own failure, on its first run in CI.
//
// The orchestrator workflow lacked `actions: read`, so every `gh run list` was
// a 403 -- and the checker collapsed "GitHub refused the question" into "this
// workflow has never run". It announced that all ten agents were dead.
//
// A health check that cries wolf when it cannot look is exactly as useless as
// one that stays quiet when it can, and it was built to fix the second problem.
func TestBeingRefusedIsNotTheSameAsNeverHavingRun(t *testing.T) {
	refused := func(string) (Run, bool, error) {
		return Run{}, false, errors.New("gh run list: exit status 1")
	}
	hs := CheckFleet(time.Now().UTC(), refused)
	for _, h := range hs {
		if h.Agent.Workflow == "" {
			continue // genuinely unscheduled, and knowable without GitHub
		}
		if h.State != HealthUnknown {
			t.Fatalf("%s: a refused query is %q, not %q", h.Agent.Name, HealthUnknown, h.State)
		}
		if h.Loud() {
			t.Fatalf("%s: an unknown must not be reported as a problem with the fleet", h.Agent.Name)
		}
	}
	if line := healthLine(hs); !strings.Contains(line, "could not check") {
		t.Fatalf("the summary must say we do not know, got %q", line)
	}
}

// An empty list from a working query still means never run. Losing that would
// trade one blind spot for another.
func TestAnEmptyAnswerStillMeansNeverRun(t *testing.T) {
	hs := CheckFleet(time.Now().UTC(), func(string) (Run, bool, error) { return Run{}, false, nil })
	for _, h := range hs {
		if h.Agent.Workflow != "" && h.State != HealthNever {
			t.Fatalf("%s: want %q, got %q", h.Agent.Name, HealthNever, h.State)
		}
	}
}

// --- too early is not broken ------------------------------------------------
//
// The first reading after publishing was one view across six games, hours old,
// and the funnel called reach broken. True, and useless: nothing had had time
// to happen. Declaring a stage broken on its first day is the mirror image of
// reporting an unknown as a zero, and this file already has three tests about
// the other direction.

func young(daysLive int) World {
	w := healthy()
	w.ItchViews7d, w.ItchPlays7d = 1, 0 // dismal, by any standard
	w.LiveSince = w.Now.AddDate(0, 0, -daysLive)
	return w
}

func TestADayOldListingIsNotAFailingOne(t *testing.T) {
	stages, verdict, action := Diagnose(young(0))
	if !strings.Contains(verdict, "too early") {
		t.Fatalf("want a too-early verdict, got %q", verdict)
	}
	if !strings.Contains(action, "nothing is wrong yet") {
		t.Fatalf("the action must not send someone to fix a non-problem: %q", action)
	}
	var reach Stage
	for _, s := range stages {
		if s.Name == "reach" {
			reach = s
		}
	}
	if !reach.TooEarly {
		t.Fatal("reach should be flagged too early")
	}
	if !reach.Known {
		t.Fatal("the number WAS read — too early is not the same as unknown")
	}
}

// ...and the patience runs out. The same numbers a week later are a finding.
func TestTheSameNumbersLaterAreAFailure(t *testing.T) {
	_, verdict, action := Diagnose(young(minReachDays + 1))
	if !strings.HasPrefix(verdict, "reach") || strings.Contains(verdict, "too early") {
		t.Fatalf("after %d days this is a real failure, got %q", minReachDays+1, verdict)
	}
	if !strings.Contains(action, "Reddit") {
		t.Fatalf("want the reach remedy, got %q", action)
	}
}

// The diagnosis must stop at a too-early stage rather than reading past it:
// every later stage depends on this one having had a chance, so judging them
// would be measuring the clock in four more places.
func TestTooEarlyStopsTheDiagnosisRatherThanFallingThrough(t *testing.T) {
	_, verdict, _ := Diagnose(young(0))
	for _, later := range []string{"click", "play", "return", "revenue"} {
		if strings.Contains(verdict, later) {
			t.Fatalf("the diagnosis read past a too-early stage into %q: %q", later, verdict)
		}
	}
}

// An unknown publication date must not become a permanent excuse.
func TestWithNoPublicationDateItIsJudgedNormally(t *testing.T) {
	w := young(0)
	w.LiveSince = time.Time{}
	_, verdict, _ := Diagnose(w)
	if strings.Contains(verdict, "too early") {
		t.Fatal("not knowing when it went live must not excuse the numbers forever")
	}
}

// A stage that is genuinely fine is never called too early, however new it is.
func TestAHealthyStageIsNeverTooEarly(t *testing.T) {
	w := healthy()
	w.LiveSince = w.Now // published seconds ago, and doing well
	_, verdict, _ := Diagnose(w)
	if strings.Contains(verdict, "too early") {
		t.Fatalf("good numbers need no patience: %q", verdict)
	}
}

// --- what happens when an agent fails ---------------------------------------

func healthOf(name string, st HealthState) Health {
	return Health{Agent: Agent{Name: name, Workflow: "x.yml"}, State: st, Detail: string(st)}
}

// Retry once, then escalate once, then stop. An agent that files an issue a day
// about the same fault is an agent you learn to filter.
func TestRetryThenEscalateThenStop(t *testing.T) {
	hs := []Health{healthOf("Yield", HealthFailing)}
	m := HealthMemory{Seen: map[string]*AgentTrouble{}}

	a1, m := Decide(hs, m, "2026-09-01")
	if len(a1) != 1 || a1[0].Kind != "retry" {
		t.Fatalf("first sighting should retry, got %+v", a1)
	}

	a2, m := Decide(hs, m, "2026-09-02")
	if len(a2) != 1 || a2[0].Kind != "escalate" {
		t.Fatalf("still failing should escalate, got %+v", a2)
	}
	m.Seen["Yield"].IssueURL = "https://example/1"

	a3, _ := Decide(hs, m, "2026-09-03")
	if len(a3) != 0 {
		t.Fatalf("already filed: it must go quiet, got %+v", a3)
	}
}

// Recovery closes a loop the person was left holding.
func TestRecoveryIsReportedAndForgotten(t *testing.T) {
	m := HealthMemory{Seen: map[string]*AgentTrouble{
		"Yield": {State: "failing", Failures: 2, Retried: true, IssueURL: "u", FirstSeen: "2026-09-01"},
	}}
	acts, m := Decide([]Health{healthOf("Yield", HealthOK)}, m, "2026-09-04")
	if len(acts) != 1 || acts[0].Kind != "recovered" {
		t.Fatalf("want a recovery, got %+v", acts)
	}
	if _, still := m.Seen["Yield"]; still {
		t.Fatal("a recovered agent must be forgotten, or it can never be escalated again")
	}
}

// The health check once announced that all ten agents were dead because it
// could not ask GitHub. Retrying ten agents on the back of a 403 would have
// turned that mistake into twenty wasted runs.
func TestUnknownIsNotActedOn(t *testing.T) {
	acts, m := Decide([]Health{healthOf("Yield", HealthUnknown)},
		HealthMemory{Seen: map[string]*AgentTrouble{}}, "2026-09-01")
	if len(acts) != 0 {
		t.Fatalf("no information is not a finding, got %+v", acts)
	}
	if len(m.Seen) != 0 {
		t.Fatal("an unknown must not be remembered as trouble")
	}
}

// Re-running a workflow that does not exist is not a retry, it is a loop.
func TestAnUnscheduledAgentIsEscalatedNotRetried(t *testing.T) {
	acts, _ := Decide([]Health{{Agent: Agent{Name: "FinOps"}, State: HealthUnscheduled,
		Detail: "built, and nothing runs it"}},
		HealthMemory{Seen: map[string]*AgentTrouble{}}, "2026-09-01")
	if len(acts) != 1 || acts[0].Kind != "escalate" {
		t.Fatalf("want escalate without a retry, got %+v", acts)
	}
}

// The first real escalation produced this title:
//
//	"Horizon agent is last run of horizon.yml: failure"
//
// which is a sentence only a program would write. The state belongs in the
// title and the detail belongs in the body, and keeping them in separate
// fields is what makes that possible.
func TestAnEscalationCarriesAStateAndADetailSeparately(t *testing.T) {
	acts, _ := Decide([]Health{{
		Agent:  Agent{Name: "Horizon", Workflow: "horizon.yml"},
		State:  HealthFailing,
		Detail: "last run of horizon.yml: failure",
	}}, HealthMemory{Seen: map[string]*AgentTrouble{"Horizon": {
		State: "failing", Retried: true, FirstSeen: "2026-08-31",
	}}}, "2026-09-01")

	if len(acts) != 1 || acts[0].Kind != "escalate" {
		t.Fatalf("want an escalation, got %+v", acts)
	}
	a := acts[0]
	if a.State != string(HealthFailing) {
		t.Fatalf("the state must be one word for a title, got %q", a.State)
	}
	if a.Reason == a.State {
		t.Fatal("the detail and the state must not be the same field")
	}
	title := a.Agent + " agent is " + a.State
	if title != "Horizon agent is failing" {
		t.Fatalf("title reads badly: %q", title)
	}
}
