package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Which of the other agents can actually produce something today.
//
// The fleet was built for a system with traffic, and there is no traffic yet,
// so most of it has nothing to say. But not all of it: an agent whose input is
// the outside world -- what other games look like, what people are asking for,
// what the ad tech press is doing -- is fully employed on day zero, and leaving
// it idle because the *other* agents are idle is a scheduling mistake rather
// than a data one.
//
// So the orchestrator reports runnable-now separately from blocked-on-data, and
// says what the blocked ones are waiting for. "The fleet is quiet" is only
// reassuring if you know which half of it is quiet on purpose.

type Agent struct {
	Name string
	Tool string
	Asks string // the question it answers

	// Workflow is the file that actually runs it, and EveryHours is how often
	// it is supposed to. An agent with no workflow is BUILT but not RUNNING,
	// which is a different and quieter kind of broken than a failing one.
	Workflow   string
	EveryHours int

	// NeedsTraffic agents are blocked until the site has real events.
	NeedsTraffic bool
	// NeedsWeb agents read the outside world, and can run from day one.
	NeedsWeb bool
	Produces string
}

func fleet() []Agent {
	// Keyed fields on purpose. A positional literal broke silently the moment
	// two fields were inserted in the middle of the struct, and the compiler
	// only caught it because the types happened to differ.
	return []Agent{
		{Name: "Orchestrator", Tool: "tools/orchestratorctl", Workflow: "orchestrator.yml", EveryHours: 24,
			Asks: "what should I do now", Produces: "this digest"},
		{Name: "Yield", Tool: "tools/yieldctl", Workflow: "agent-proposals.yml", EveryHours: 24,
			Asks: "which arm of an experiment is winning", NeedsTraffic: true,
			Produces: "a control-plane pull request"},
		{Name: "Integrity", Tool: "tools/integrityctl", Workflow: "nightly-report.yml", EveryHours: 24,
			Asks: "which impressions should not have been billed", NeedsTraffic: true,
			Produces: "revocations"},
		{Name: "FinOps", Tool: "tools/finopsctl", Workflow: "fleet.yml", EveryHours: 24,
			Asks: "what each publisher and buyer costs us", NeedsTraffic: true,
			Produces: "cost attribution"},
		{Name: "Demand", Tool: "tools/demandctl", Workflow: "fleet.yml", EveryHours: 24,
			Asks: "which buyers are worth the call", NeedsTraffic: true,
			Produces: "buyer scorecards"},
		{Name: "Reliability", Tool: "tools/reliabilityctl", Workflow: "reliability.yml", EveryHours: 12,
			Asks: "can we afford to stay up", Produces: "alarms and spend, twice a day"},
		{Name: "Product", Tool: "tools/productctl", Workflow: "fleet.yml", EveryHours: 24,
			Asks: "what should we build next", NeedsWeb: true,
			Produces: "game and feature proposals, benchmarked against what is out there"},
		{Name: "Feedback", Tool: "tools/feedbackctl", Workflow: "agent-proposals.yml", EveryHours: 24,
			Asks: "what are players asking for", NeedsWeb: true,
			Produces: "itch and Reddit comments the data agrees with"},
		{Name: "Horizon", Tool: "tools/horizonctl", Workflow: "horizon.yml", EveryHours: 168,
			Asks: "what is the rest of the world playing", NeedsWeb: true,
			Produces: "a weekly scan, and a pull request when it moves"},
		{Name: "Supply chain", Tool: "tools/supplychain", Workflow: "fleet.yml", EveryHours: 24,
			Asks: "is our ads.txt / sellers.json chain intact", NeedsWeb: true,
			Produces: "verification"},
	}
}

// FleetSchedule splits the fleet by what is stopping it, which is the only
// division a person needs in order to act.
func FleetSchedule(w World) (runnable, waiting []Agent) {
	hasTraffic := w.Sessions7d > 0
	for _, a := range fleet() {
		if a.NeedsTraffic && !hasTraffic {
			waiting = append(waiting, a)
			continue
		}
		runnable = append(runnable, a)
	}
	return runnable, waiting
}

func fleetLine(w World) string {
	runnable, waiting := FleetSchedule(w)
	if len(waiting) == 0 {
		return fmt.Sprintf("all %d agents have data to work with", len(runnable))
	}
	return fmt.Sprintf("%d agents can run today; %d are waiting for the first real traffic",
		len(runnable), len(waiting))
}

// --- is the fleet actually running? ----------------------------------------
//
// This exists because of a single day in which three agents were found to be
// silently dead while this very report said "4 agents can run today". It was
// answering whether they COULD run. Nobody was asking whether they HAD.
//
//   - The Orchestrator's own digest was built and thrown away for want of a
//     file path, and the workflow went green.
//   - Every Reliability alert had done the same since the day it was written.
//   - agent-proposals.yml had never parsed, so the Yield and Feedback agents
//     had never executed once.
//
// All three were invisible in exactly the same way: nothing failed loudly. An
// agent that cannot report its own liveness is an agent you find out about on
// the day you needed it.

type HealthState string

const (
	HealthOK          HealthState = "ok"
	HealthUnscheduled HealthState = "not scheduled"
	HealthNever       HealthState = "never run"
	HealthFailing     HealthState = "failing"
	HealthOverdue     HealthState = "overdue"
	HealthUnknown     HealthState = "unknown"
)

type Health struct {
	Agent  Agent
	State  HealthState
	Detail string
}

// Loud reports whether this is worth a human's attention now.
func (h Health) Loud() bool {
	return h.State == HealthNever || h.State == HealthFailing ||
		h.State == HealthOverdue || h.State == HealthUnscheduled
}

type Run struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
	CreatedAt  string `json:"createdAt"`
}

// runLookup is a function so tests do not need a network or a repository.
//
// Three return values rather than two, and the third is the whole point. An
// earlier version collapsed "GitHub refused the question" and "this workflow
// has never run" into a single false, and the first time it ran in CI it
// announced that all ten agents were dead -- because the workflow lacked
// `actions: read` and every query was a 403.
//
// A health check that cries wolf when it cannot look is exactly as useless as
// one that stays quiet when it can, and it was built to fix the second problem.
type runLookup func(workflow string) (run Run, found bool, err error)

func ghRuns(workflow string) (Run, bool, error) {
	out, err := exec.Command("gh", "run", "list", "--workflow", workflow,
		"--limit", "1", "--json", "conclusion,status,createdAt").Output()
	if err != nil {
		return Run{}, false, fmt.Errorf("gh run list: %w", err)
	}
	var runs []Run
	if err := json.Unmarshal(out, &runs); err != nil {
		return Run{}, false, fmt.Errorf("unreadable response: %w", err)
	}
	if len(runs) == 0 {
		return Run{}, false, nil // asked, answered: genuinely never run
	}
	return runs[0], true, nil
}

// overdueFactor: an agent is only called overdue at twice its cadence, so a
// single skipped nightly run is not a page. Anything that reports at the first
// missed beat gets muted, and a muted health check is worse than none.
const overdueFactor = 2

func CheckFleet(now time.Time, look runLookup) []Health {
	var out []Health
	for _, a := range fleet() {
		if a.Workflow == "" {
			out = append(out, Health{a, HealthUnscheduled,
				"built, tested, and nothing runs it"})
			continue
		}
		if look == nil {
			out = append(out, Health{a, HealthUnknown, "could not ask GitHub"})
			continue
		}
		r, found, err := look(a.Workflow)
		if err != nil {
			out = append(out, Health{a, HealthUnknown,
				"could not ask GitHub — " + err.Error()})
			continue
		}
		if !found {
			out = append(out, Health{a, HealthNever, a.Workflow + " has no runs at all"})
			continue
		}
		if r.Status == "completed" && r.Conclusion != "success" && r.Conclusion != "" {
			out = append(out, Health{a, HealthFailing,
				fmt.Sprintf("last run of %s: %s", a.Workflow, r.Conclusion)})
			continue
		}
		t, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			out = append(out, Health{a, HealthUnknown, "unreadable run date"})
			continue
		}
		age := now.Sub(t)
		if limit := time.Duration(a.EveryHours*overdueFactor) * time.Hour; age > limit {
			out = append(out, Health{a, HealthOverdue,
				fmt.Sprintf("last ran %.0fh ago, expected every %dh", age.Hours(), a.EveryHours)})
			continue
		}
		out = append(out, Health{a, HealthOK,
			fmt.Sprintf("ran %.0fh ago", age.Hours())})
	}
	return out
}

func Unhealthy(hs []Health) []Health {
	var out []Health
	for _, h := range hs {
		if h.Loud() {
			out = append(out, h)
		}
	}
	return out
}

func healthLine(hs []Health) string {
	bad := Unhealthy(hs)
	unknown := 0
	for _, h := range hs {
		if h.State == HealthUnknown {
			unknown++
		}
	}
	if len(hs) == 0 {
		return "could not check — GitHub was not reachable"
	}
	// Said plainly, and never dressed up as either health or failure. The
	// honest report is that we do not know.
	if unknown == len(hs) {
		return fmt.Sprintf("could not check any of the %d agents — %s", len(hs),
			"the workflow needs `actions: read`")
	}
	if unknown > 0 {
		return fmt.Sprintf("%d of %d agents are not running; %d could not be checked",
			len(bad), len(hs), unknown)
	}
	if len(bad) == 0 {
		return fmt.Sprintf("all %d agents have run recently", len(hs))
	}
	return fmt.Sprintf("%d of %d agents are not running", len(bad), len(hs))
}
