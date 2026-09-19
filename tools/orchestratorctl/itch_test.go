package main

import (
	"testing"
	"time"
)

func day(t time.Time, back int) string {
	return t.AddDate(0, 0, -back).Format("2006-01-02")
}

// The API reports LIFETIME totals; the funnel asks about a week. A lifetime
// total is indistinguishable from a very good week right up until it stops
// moving, which is the wrong moment to find out.
func TestAWeeklyFigureIsADeltaNotATotal(t *testing.T) {
	now := time.Now().UTC()
	h := ItchHistory{Readings: []Reading{
		{Date: day(now, 10), Views: 100},
		{Date: day(now, 7), Views: 140},
		{Date: day(now, 0), Views: 200},
	}}
	// 200 today against 140 a week ago.
	if got := Views7d(h, now, time.Time{}); got != 60 {
		t.Fatalf("want 60, got %d", got)
	}
}

// Before a week of history exists, the lifetime total IS the week -- but only
// while the pages themselves are younger than a week. That is checked, not
// assumed, because it stops being true silently.
func TestYoungPagesMayUseTheLifetimeTotal(t *testing.T) {
	now := time.Now().UTC()
	h := ItchHistory{Readings: []Reading{{Date: day(now, 0), Views: 12}}}

	if got := Views7d(h, now, now.AddDate(0, 0, -2)); got != 12 {
		t.Fatalf("pages 2 days old: the total is the week, want 12, got %d", got)
	}
	if got := Views7d(h, now, now.AddDate(0, 0, -40)); got != -1 {
		t.Fatalf("pages 40 days old with one reading: unknowable, want -1, got %d", got)
	}
	if got := Views7d(h, now, time.Time{}); got != -1 {
		t.Fatalf("no publication date and no history: unknowable, want -1, got %d", got)
	}
}

// Counts going backwards means something is wrong -- a deleted project, a
// changed account. Reporting a negative week would be worse than saying nothing.
func TestCountsGoingBackwardsSayNothing(t *testing.T) {
	now := time.Now().UTC()
	h := ItchHistory{Readings: []Reading{
		{Date: day(now, 8), Views: 500},
		{Date: day(now, 0), Views: 100},
	}}
	if got := Views7d(h, now, time.Time{}); got != -1 {
		t.Fatalf("want -1, got %d", got)
	}
}

// A draft is invisible to everyone but its owner, so it is not reach.
func TestDraftsAreNotCounted(t *testing.T) {
	r := RecordReading(ItchHistory{}, []itchGame{
		{Title: "live", Views: 30, Published: true},
		{Title: "hidden", Views: 9, Published: false},
	}, "2026-08-30")
	if len(r.Readings) != 1 || r.Readings[0].Views != 30 {
		t.Fatalf("want 30 from the published game only, got %+v", r.Readings)
	}
	if _, ok := r.Readings[0].PerGame["hidden"]; ok {
		t.Fatal("a draft was recorded")
	}
}

// Running twice in a day must not create two readings for that day, or the
// delta is computed against this morning.
func TestASecondRunReplacesTodayRatherThanAppending(t *testing.T) {
	h := RecordReading(ItchHistory{}, []itchGame{{Title: "g", Views: 5, Published: true}}, "2026-08-30")
	h = RecordReading(h, []itchGame{{Title: "g", Views: 8, Published: true}}, "2026-08-30")
	if len(h.Readings) != 1 {
		t.Fatalf("want one reading for the day, got %d", len(h.Readings))
	}
	if h.Readings[0].Views != 8 {
		t.Fatalf("want the later figure, got %d", h.Readings[0].Views)
	}
}

func TestReadingsStaySorted(t *testing.T) {
	h := RecordReading(ItchHistory{}, []itchGame{{Title: "g", Views: 2, Published: true}}, "2026-08-30")
	h = RecordReading(h, []itchGame{{Title: "g", Views: 1, Published: true}}, "2026-08-29")
	if h.Readings[0].Date != "2026-08-29" {
		t.Fatalf("out of order: %+v", h.Readings)
	}
}
