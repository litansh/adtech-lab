package agentkit

import "fmt"

// Stage is the maturity ladder. Every agent supports every stage, and the stage
// is a flag rather than a different build -- so "read-only" is a property of the
// running process, not a promise about which binary someone deployed.
type Stage int

const (
	// StageObserve reads and reports. No write path is reachable. Read-only
	// credentials are sufficient, which is the entire adoption pitch: most
	// organisations will say no to an autonomous agent and yes to a report.
	StageObserve Stage = iota

	// StageRecommend emits a proposal a human applies. The human is the
	// actuator, so the agent still cannot act.
	StageRecommend

	// StageShadow computes what it WOULD have done and records it beside what
	// actually happened. This is the stage everyone wants to skip and the only
	// one that produces evidence a sceptic can act on.
	StageShadow

	// StageBoundedAct applies changes within hard limits, with a holdout.
	StageBoundedAct

	// StageAct is full autonomy inside guardrails.
	StageAct
)

var stageNames = map[Stage]string{
	StageObserve: "observe", StageRecommend: "recommend", StageShadow: "shadow",
	StageBoundedAct: "bounded-act", StageAct: "act",
}

func (s Stage) String() string {
	if n, ok := stageNames[s]; ok {
		return n
	}
	return "unknown"
}

func ParseStage(s string) (Stage, error) {
	for k, v := range stageNames {
		if v == s {
			return k, nil
		}
	}
	return StageObserve, fmt.Errorf("unknown stage %q (observe|recommend|shadow|bounded-act|act)", s)
}

// CanWrite reports whether this stage may mutate anything outside its own
// report. Observe, Recommend and Shadow must not: Recommend emits a proposal
// for a human, and Shadow records a counterfactual, neither of which is a
// change to the system being observed.
func (s Stage) CanWrite() bool { return s >= StageBoundedAct }

// GuardWrite is called immediately before any mutation. It returns an error
// rather than relying on the caller having checked, because "we only call this
// in act mode" is exactly the assumption that stops being true during a
// refactor.
func (s Stage) GuardWrite(what string) error {
	if s.CanWrite() {
		return nil
	}
	return fmt.Errorf("refusing to %s: stage is %q, which must not write. "+
		"Promote deliberately -- see docs/portable-agents.md", what, s)
}
