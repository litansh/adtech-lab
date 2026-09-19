package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// What the Orchestrator DOES about a sick agent, rather than merely noticing.
//
// Detection alone was the previous state: an unhealthy agent got a line in the
// morning digest, and if that line went unread, nothing happened -- forever. A
// health check nobody acts on is a health check that only converts a silent
// failure into a quiet one.
//
// The ladder is deliberately short, and it stops well before anything
// interesting:
//
//   1. RETRY, once. Most of these agents are read-only reports, and a run that
//      failed on a rate limit or a network blip succeeds on the second attempt.
//      Re-running something is not a change to the system.
//   2. ESCALATE, once. Still failing after a retry means it will not fix
//      itself, so it becomes an issue with a label and its own message --
//      out of the digest, where it can be lost, and into a list.
//   3. STOP. It never retries a third time and never files a second issue.
//      An agent that files an issue a day about the same fault is an agent
//      that trains you to filter it.
//
// What it will not do is repair anything. A failing agent is usually failing
// because of code, and code is architecture, and architecture is where agents
// stop. It re-runs and it reports; a human fixes.

type HealthMemory struct {
	// Agent name -> what we have already done about it.
	Seen map[string]*AgentTrouble `json:"seen"`
}

type AgentTrouble struct {
	State     string `json:"state"`
	Failures  int    `json:"consecutive_failures"`
	Retried   bool   `json:"retried"`
	IssueURL  string `json:"issue_url,omitempty"`
	FirstSeen string `json:"first_seen"`
}

func LoadHealthMemory(path string) HealthMemory {
	m := HealthMemory{Seen: map[string]*AgentTrouble{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	if json.Unmarshal(b, &m) != nil || m.Seen == nil {
		m.Seen = map[string]*AgentTrouble{}
	}
	return m
}

func SaveHealthMemory(path string, m HealthMemory) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Action is what to do about one agent, decided from its state and what has
// already been tried.
type Action struct {
	Agent    string
	Kind     string // "retry" | "escalate" | "recovered" | "none"
	Workflow string
	// State is one word for a title; Reason is the detail for a body. Keeping
	// them apart is why the title reads as English.
	State  string
	Reason string
}

// retryable states. `not scheduled` is excluded on purpose: re-running a
// workflow that does not exist is not a retry, it is a loop, and the fix is a
// human writing one.
func retryable(state HealthState) bool {
	return state == HealthFailing || state == HealthOverdue
}

// Decide walks the fleet's health against what has already been done.
//
// `unknown` is treated as "no information" rather than as trouble -- the health
// check announced a whole dead fleet once, because it could not ask GitHub, and
// retrying ten agents on the back of a 403 would have turned that mistake into
// twenty wasted runs.
func Decide(hs []Health, m HealthMemory, day string) ([]Action, HealthMemory) {
	if m.Seen == nil {
		m.Seen = map[string]*AgentTrouble{}
	}
	var out []Action

	healthy := map[string]bool{}
	for _, h := range hs {
		name := h.Agent.Name

		if h.State == HealthOK {
			healthy[name] = true
			if prev, ok := m.Seen[name]; ok {
				// Recovery is worth saying out loud: it closes a loop the
				// person was left holding.
				out = append(out, Action{name, "recovered", h.Agent.Workflow, prev.State,
					fmt.Sprintf("was %s since %s", prev.State, prev.FirstSeen)})
				delete(m.Seen, name)
			}
			continue
		}
		if h.State == HealthUnknown {
			continue // no information is not a finding about the agent
		}

		prev := m.Seen[name]
		if prev == nil {
			prev = &AgentTrouble{State: string(h.State), FirstSeen: day}
			m.Seen[name] = prev
		}
		prev.Failures++
		prev.State = string(h.State)

		switch {
		case retryable(h.State) && !prev.Retried:
			prev.Retried = true
			out = append(out, Action{name, "retry", h.Agent.Workflow, string(h.State), h.Detail})
		case prev.IssueURL == "":
			out = append(out, Action{name, "escalate", h.Agent.Workflow, string(h.State), h.Detail})
		default:
			// Already filed. Saying it again every morning is how a real
			// finding gets filtered out along with the noise.
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Agent < out[j].Agent })
	return out, m
}

// --- carrying them out ------------------------------------------------------

func Retry(workflow string) error {
	if workflow == "" {
		return fmt.Errorf("no workflow to retry")
	}
	out, err := exec.Command("gh", "workflow", "run", workflow).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", workflow, strings.TrimSpace(string(out)))
	}
	return nil
}

func Escalate(a Action) (string, error) {
	body := fmt.Sprintf(`The **%s** agent is %s, and a retry did not fix it.

    %s

Raised by the Orchestrator, which retried once and then stopped. It will not
file this again — an agent that reports the same fault every morning is one you
learn to filter.

## What it will not do

Fix it. A failing agent is usually failing because of code, and code is
architecture, and architecture is where agents stop.

## Where to look

    gh run list --workflow %s --limit 5
    gh run view --log-failed

Close this when the agent runs green; the Orchestrator will confirm the recovery
in the next morning's briefing.
`, a.Agent, a.State, a.Reason, a.Workflow)

	f, err := os.CreateTemp("", "escalate-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	// The title carries the STATE; the detail belongs in the body. Using the
	// detail produced "Horizon agent is last run of horizon.yml: failure",
	// which is a sentence only a program would write.
	out, err := exec.Command("gh", "issue", "create",
		"--title", fmt.Sprintf("%s agent is %s", a.Agent, a.State),
		"--body-file", f.Name(), "--label", "agent").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
