package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func assessed(id string, st Status) Assessment {
	return Assessment{Step: Step{ID: id, Title: "step " + id, Phase: "Today", Minutes: 5}, Status: st}
}

// Announced once. An agent that congratulates you every morning for something
// you did last week is one you learn to ignore, and then it cannot tell you the
// thing that mattered.
func TestAnAchievementFiresExactlyOnce(t *testing.T) {
	as := []Assessment{assessed("a", StatusDone), assessed("b", StatusReady)}

	p := Progress{Done: map[string]string{}}
	if got := Achievements(p, as); len(got) != 1 || got[0].Step.ID != "a" {
		t.Fatalf("first run should announce a, got %v", got)
	}

	p = Record(p, as, "2026-08-29")
	if got := Achievements(p, as); len(got) != 0 {
		t.Fatalf("second run announced it again: %v", got)
	}
}

// itch.io is unreachable for an hour, the step reads as not-done, then comes
// back. That is a network blip, not a fresh accomplishment.
func TestAFlickerDoesNotReAnnounce(t *testing.T) {
	done := []Assessment{assessed("a", StatusDone)}
	p := Record(Progress{Done: map[string]string{}}, done, "2026-08-29")

	// The check could not run today, so the step is not reported done...
	p = Record(p, []Assessment{assessed("a", StatusBlocked)}, "2026-08-30")
	// ...and tomorrow it is back.
	if got := Achievements(p, done); len(got) != 0 {
		t.Fatalf("a flicker was announced as new: %v", got)
	}
	if p.Done["a"] != "2026-08-29" {
		t.Fatalf("the original date must survive, got %q", p.Done["a"])
	}
}

// The cheerful version of this repository's recurring bug: celebrating work
// that did not happen. A conflict is a step someone marked done that the world
// disagrees with, which is the opposite of an achievement.
func TestAConflictIsNeverAnAchievement(t *testing.T) {
	as := []Assessment{assessed("a", StatusConflict)}
	if got := Achievements(Progress{Done: map[string]string{}}, as); len(got) != 0 {
		t.Fatalf("celebrated a conflict: %v", got)
	}
	p := Record(Progress{Done: map[string]string{}}, as, "2026-08-29")
	if p.Done["a"] != "" {
		t.Fatal("a conflict was recorded as finished")
	}
}

func TestRoadmapCarriesTheRealDatesAndTheNextStep(t *testing.T) {
	as := []Assessment{assessed("a", StatusDone), assessed("b", StatusReady)}
	p := Record(Progress{Done: map[string]string{}}, as, "2026-08-29")
	w := base()
	w.LiveGames = 0

	out := RenderRoadmap(as, p, w)
	for _, want := range []string{"done 2026-08-29", "1 of 2 steps", "## Next", "step b"} {
		if !strings.Contains(out, want) {
			t.Errorf("roadmap is missing %q", want)
		}
	}
	if strings.Contains(out, "Do not edit") == false {
		t.Error("a generated file must say it is generated")
	}
}

// A missing file is the first run, not a failure -- and it must not read as
// "everything was just achieved" either, which would announce all ten steps.
func TestNoProgressFileYetIsAnEmptyHistory(t *testing.T) {
	p := LoadProgress(filepath.Join(t.TempDir(), "absent.json"))
	if p.Done == nil {
		t.Fatal("Done must be usable, not nil")
	}
	if len(p.Done) != 0 {
		t.Fatal("a missing file is an empty history")
	}
}

func TestProgressSurvivesARoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	in := Progress{Done: map[string]string{"a": "2026-08-29"}}
	if err := SaveProgress(path, in); err != nil {
		t.Fatal(err)
	}
	if got := LoadProgress(path); got.Done["a"] != "2026-08-29" {
		t.Fatalf("got %v", got.Done)
	}
}

func TestTodayIsUTC(t *testing.T) {
	// 23:30 in a +03:00 zone is already tomorrow in UTC, and the schedule,
	// the dedupe and this must all agree on which day it is.
	tt := time.Date(2026, 8, 29, 23, 30, 0, 0, time.FixedZone("IDT", 3*3600))
	if got := today(tt); got != "2026-08-29" {
		t.Fatalf("want the UTC date 2026-08-29, got %s", got)
	}
}

// Fourth instance in one day of a tool assuming its working directory is the
// repository root. Here it wrote ROADMAP.md inside tools/orchestratorctl,
// exited zero, and printed a perfectly good report -- so nothing looked wrong.
func TestDefaultPathsLandInTheRepositoryNotTheToolDirectory(t *testing.T) {
	// Tests run with the tool as the working directory, exactly as CI does.
	root := repoRoot()
	if root == "." {
		t.Fatal("could not find the repository root from the tool directory")
	}
	for _, rel := range []string{"PLAN.md", "state/plan.json"} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s is not at the discovered root %s", rel, root)
		}
	}
	if strings.HasSuffix(root, "orchestratorctl") {
		t.Fatal("the tool directory was mistaken for the repository root")
	}
}
