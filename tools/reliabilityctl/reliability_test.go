package main

import (
	"os"
	"strings"
	"testing"
)

func base() Signals {
	return Signals{
		MonthToDateUSD: 1.20, DaysIntoMonth: 15, DaysInMonth: 30, CeilingUSD: 100,
		CreditsLeftUSD: 119.94, DaysToExpiry: 160,
		Invocations: 5000, Errors: 2, P95LatencyMS: 90,
		Alarms: map[string]string{"budget-80": "OK", "errors": "OK"},
	}
}

func find(fs []Finding, check string) *Finding {
	for i := range fs {
		if fs[i].Check == check {
			return &fs[i]
		}
	}
	return nil
}

func TestAQuietSystemPagesNobody(t *testing.T) {
	fs := Assess(base())
	if w := Worst(fs); w != SevOK {
		t.Fatalf("a healthy system produced %s: %+v", w, fs)
	}
}

// There is no hard monetary cap on this stack, so a projection past the
// ceiling is the one financial signal worth an interruption.
func TestProjectedOverspendPages(t *testing.T) {
	s := base()
	s.MonthToDateUSD = 70 // 15 days in -> projects $140 against a $100 ceiling
	fs := Assess(s)
	f := find(fs, "spend")
	if f == nil || f.Severity != SevPage {
		t.Fatalf("projecting $140 of a $100 ceiling did not page: %+v", f)
	}
	if !strings.Contains(f.Action, "no hard cap") {
		t.Errorf("the action should say why this matters: %q", f.Action)
	}
}

// And a mild overshoot must not, or the agent gets muted.
func TestAMildOvershootIsANoteNotAPage(t *testing.T) {
	s := base()
	s.MonthToDateUSD = 42 // projects $84 -> 84% of the ceiling
	f := find(Assess(s), "spend")
	if f == nil || f.Severity != SevNote {
		t.Fatalf("84%% of the ceiling should be a note, got %+v", f)
	}
	if !strings.Contains(f.Action, "not worth waking up") {
		t.Errorf("action: %q", f.Action)
	}
}

// Credits running out BEFORE they expire changes the plan, not just the budget.
func TestCreditsExhaustedBeforeExpiryIsFlagged(t *testing.T) {
	s := base()
	s.MonthToDateUSD = 30 // $2/day -> ~60 days of credit, but 160 days to expiry
	f := find(Assess(s), "credits")
	if f == nil {
		t.Fatal("credits running out months before expiry was not flagged")
	}
	if !strings.Contains(f.Action, "sooner than the date suggests") {
		t.Errorf("the action should say the deadline moved: %q", f.Action)
	}
}

// Three tiers, and the bottom one is the important one: a couple of failures
// in five thousand requests is a bot hitting a bad URL, and reporting it
// trains the reader to skim.
func TestErrorsHaveThreeTiers(t *testing.T) {
	s := base()

	s.Errors = 3 // 0.06% -- below the noise floor
	if f := find(Assess(s), "errors"); f != nil {
		t.Fatalf("0.06%% errors produced a finding: %+v", f)
	}

	s.Errors = 100 // 2% -- worth reading tomorrow
	if f := find(Assess(s), "errors"); f == nil || f.Severity != SevNote {
		t.Fatalf("2%% errors should be a note: %+v", f)
	}

	s.Errors = 400 // 8% -- something is broken
	if f := find(Assess(s), "errors"); f == nil || f.Severity != SevPage {
		t.Fatalf("8%% errors should page: %+v", f)
	}
}

// Too few invocations to have a rate at all.
func TestErrorRateIsNotJudgedOnTinyVolume(t *testing.T) {
	s := base()
	s.Invocations = 20
	s.Errors = 4 // 20%, but on 20 requests
	if f := find(Assess(s), "errors"); f != nil {
		t.Fatalf("judged an error rate on 20 invocations: %+v", f)
	}
}

// An alarm that has never had data will never fire. That is worse than not
// having it, because it stops anyone looking.
func TestAnAlarmWithNoDataIsReportedAsNotProtection(t *testing.T) {
	s := base()
	s.Alarms["fuse-trip"] = "INSUFFICIENT_DATA"
	f := find(Assess(s), "alarms")
	if f == nil {
		t.Fatal("an alarm with no data was not reported")
	}
	if !strings.Contains(f.Action, "appearance of protection") {
		t.Errorf("the action should say what it actually is: %q", f.Action)
	}
}

func TestFiringAlarmsPageAndSortFirst(t *testing.T) {
	s := base()
	s.Alarms["errors"] = "ALARM"
	s.MonthToDateUSD = 42 // also produces a NOTE
	fs := Assess(s)
	if fs[0].Severity != SevPage {
		t.Fatalf("a firing alarm did not sort first: %+v", fs)
	}
	if Worst(fs) != SevPage {
		t.Error("worst severity is not PAGE")
	}
}

// The property the whole severity split exists for.
func TestNotEverythingIsUrgent(t *testing.T) {
	s := base()
	s.MonthToDateUSD = 42
	s.Errors = 100 // 2%: a note, not a page
	s.Alarms["fuse-trip"] = "INSUFFICIENT_DATA"
	fs := Assess(s)
	if Worst(fs) != SevNote {
		t.Fatalf("three mild issues produced %s -- an agent that pages for "+
			"everything gets muted, and a muted reliability agent is the most "+
			"dangerous object in the system", Worst(fs))
	}
}

// The confusion that matters most for this agent: silence is what a BROKEN
// agent produces, so "all fine" and "cannot see" must never look the same.
func TestNoAlarmsReadIsNotTheSameAsNoProblems(t *testing.T) {
	s := base()
	s.Alarms = map[string]string{}
	f := find(Assess(s), "alarms")
	if f == nil || f.Severity != SevNote {
		t.Fatalf("reading zero alarms produced %+v -- it must say so", f)
	}
	if !strings.Contains(f.Action, "cannot see") {
		t.Errorf("the action must distinguish blindness from health: %q", f.Action)
	}

	// And a healthy read says how many, so the reader can tell.
	s.Alarms = map[string]string{"a": "OK", "b": "OK"}
	f = find(Assess(s), "alarms")
	if f == nil || f.Severity != SevOK || !strings.Contains(f.Detail, "2 alarms") {
		t.Fatalf("a healthy read should report the count: %+v", f)
	}
}

// The Reliability agent -- the one whose entire job is paging a human when
// spend or alarms go wrong -- had never delivered a single message. It built a
// command with a path relative to the repository root while running with the
// tool as its working directory, and discarded the result.
//
// An alerting agent that fails silently is worse than no alerting agent,
// because it also produces the belief that someone is watching.
func TestNotifyScriptResolvesFromTheToolDirectory(t *testing.T) {
	got, err := notifyScript()
	if err != nil {
		t.Fatalf("could not find telegram.py from the tool directory: %v", err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("found %s, which does not exist", got)
	}
}
