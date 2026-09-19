package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func atHour(h, m int) time.Time {
	return time.Date(2026, 8, 29, h, m, 0, 0, time.UTC)
}

func paced(budget float64) *LineItem {
	return &LineItem{ID: "li1", DailyBudget: budget, Pacing: PacingEven}
}

func TestOnScheduleSpendIsNotThrottled(t *testing.T) {
	li := paced(100)
	// Midday, half the budget spent: exactly on schedule.
	d, serve := Pace(li, 50, atHour(12, 0), rand.New(rand.NewSource(1)))
	if !serve || d.Throttled {
		t.Fatalf("on-schedule spend was throttled: %+v", d)
	}
	if math.Abs(d.Elapsed-0.5) > 0.001 {
		t.Errorf("elapsed %.4f, want 0.5", d.Elapsed)
	}
}

// Being twice ahead should halve the rate, not stop delivery.
func TestThrottleIsProportionalNotBinary(t *testing.T) {
	li := paced(100)
	// Midday, target ~52 (50% + 2% burst), spent 100: about twice ahead.
	d, _ := Pace(li, 100, atHour(12, 0), rand.New(rand.NewSource(1)))
	if !d.Throttled {
		t.Fatal("spending double the target was not throttled")
	}
	if d.Probability < 0.45 || d.Probability > 0.60 {
		t.Fatalf("serve probability %.3f, want ~0.52 (target/actual)", d.Probability)
	}

	// And it actually serves at about that rate.
	rng := rand.New(rand.NewSource(7))
	served := 0
	for i := 0; i < 4000; i++ {
		if _, ok := Pace(li, 100, atHour(12, 0), rng); ok {
			served++
		}
	}
	rate := float64(served) / 4000
	if math.Abs(rate-d.Probability) > 0.03 {
		t.Errorf("served at %.3f, want ~%.3f", rate, d.Probability)
	}
}

// A line item that stops entirely has no observations, so it cannot discover
// that conditions changed and cannot recover.
func TestThrottledLineItemNeverStopsCompletely(t *testing.T) {
	li := paced(100)
	// Wildly ahead: the entire budget spent in the first minutes.
	d, _ := Pace(li, 100, atHour(0, 30), rand.New(rand.NewSource(1)))
	if d.Probability < minServeProbability {
		t.Fatalf("probability %.4f fell below the floor %.2f", d.Probability, minServeProbability)
	}
	rng := rand.New(rand.NewSource(3))
	served := 0
	for i := 0; i < 5000; i++ {
		if _, ok := Pace(li, 100, atHour(0, 30), rng); ok {
			served++
		}
	}
	if served == 0 {
		t.Fatal("a heavily throttled line item served nothing at all")
	}
}

// Without a burst allowance, elapsed is ~0 at midnight and every line item is
// throttled to the floor for the first minutes of every day.
func TestBurstAllowanceStopsMidnightStarvation(t *testing.T) {
	li := paced(100)
	// One minute past midnight, nothing spent yet.
	d, serve := Pace(li, 0, atHour(0, 1), rand.New(rand.NewSource(1)))
	if !serve || d.Throttled {
		t.Fatalf("a line item with zero spend was throttled at 00:01: %+v", d)
	}
	// And a small amount of early spend is still allowed.
	if _, serve := Pace(li, 1.0, atHour(0, 1), rand.New(rand.NewSource(1))); !serve {
		t.Error("1% of budget at 00:01 was throttled; the burst allowance is 2%")
	}
}

func TestASAPIsNotPaced(t *testing.T) {
	li := paced(100)
	li.Pacing = PacingASAP
	d, serve := Pace(li, 999, atHour(0, 5), rand.New(rand.NewSource(1)))
	if !serve || d.Throttled {
		t.Fatalf("an ASAP line item was paced: %+v", d)
	}
}

// Pacing means "slow down", never "stop". Budget exhaustion is a separate
// check and conflating them makes the pacer silently enforce budgets.
func TestPacingDoesNotEnforceBudgets(t *testing.T) {
	li := paced(0) // no daily budget configured
	if _, serve := Pace(li, 5000, atHour(1, 0), rand.New(rand.NewSource(1))); !serve {
		t.Fatal("a line item with no daily budget was paced")
	}
}

func TestNoRandomSourceFailsTowardsDelivering(t *testing.T) {
	li := paced(100)
	if _, serve := Pace(li, 100, atHour(12, 0), nil); !serve {
		t.Fatal("with no rng, pacing suppressed delivery")
	}
}

// --- delivery projection ---

func TestProjectionSpotsUnderDelivery(t *testing.T) {
	li := paced(100)
	// Midday, only $10 spent: on this rate the day ends at $20 of a $100 budget.
	p := Project(li, 10, atHour(12, 0))
	if p.OnTrack {
		t.Fatal("spending 10% of budget by midday was reported as on track")
	}
	if math.Abs(p.Projected-20) > 0.5 {
		t.Errorf("projected %.2f, want ~20", p.Projected)
	}
	if p.Shortfall < 70 {
		t.Errorf("shortfall %.2f, want ~80", p.Shortfall)
	}
}

func TestProjectionIsQuietWhenOnTrack(t *testing.T) {
	li := paced(100)
	p := Project(li, 50, atHour(12, 0))
	if !p.OnTrack {
		t.Fatalf("on-pace delivery was flagged: %+v", p)
	}
}

// A few minutes of data extrapolated across a day produces confident nonsense.
func TestProjectionRefusesToGuessTooEarly(t *testing.T) {
	li := paced(100)
	p := Project(li, 0.01, atHour(0, 30)) // ~2% of the day
	if !math.IsNaN(p.Projected) {
		t.Fatalf("projected %.4f from 2%% of the day; should refuse", p.Projected)
	}
	if !p.OnTrack {
		t.Error("refusing to project should not report a problem it cannot see")
	}
}

// The interaction that motivated the projection: a cap and a pace are each
// reasonable and together can make a budget undeliverable.
func TestCapAndPaceTogetherCanStarveABudget(t *testing.T) {
	cp, err := loadControlPlane("fixtures/control-plane.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var li *LineItem
	for i := range cp.LineItems {
		if cp.LineItems[i].ID == "li-acme-cpm" {
			li = &cp.LineItems[i]
		}
	}
	if li == nil {
		t.Skip("li-acme-cpm not in the fixture")
	}
	li.FrequencyCap = FrequencyCap{Impressions: 3, Scope: ScopeSession}

	fs := NewMemoryFrequency(time.Hour)
	b := NewMemoryBudget()
	rng := rand.New(rand.NewSource(11))
	r := AdRequest{PlacementID: cp.Placements[0].ID, Game: "xo",
		DeviceType: "desktop", Country: "IL"}

	// Twenty sessions, each allowed at most 3 impressions of this line item.
	paidImps := 0
	for s := 0; s < 20; s++ {
		r.SessionID = "sess-" + string(rune('a'+s%26)) + string(rune('0'+s/26))
		for i := 0; i < 6; i++ {
			won, _, _ := Decide(cp, &r, &cp.Placements[0], b, fs, rng, atHour(12, 0))
			if won != nil && won.ID == li.ID {
				paidImps++
				fs.Increment(r.SessionID, li.ID)
				b.AddSpend(li.ID, li.Rate/1000)
			}
		}
	}

	spend := b.SpendToday(li.ID)
	p := Project(li, spend, atHour(12, 0))
	t.Logf("20 sessions, cap 3: %d impressions, $%.4f spent of a $%.2f budget",
		paidImps, spend, li.DailyBudget)
	t.Logf("projection at midday: %.4f, on track = %v", p.Projected, p.OnTrack)

	if paidImps != 60 {
		t.Errorf("expected exactly 20 sessions x cap 3 = 60 impressions, got %d", paidImps)
	}
	if p.OnTrack {
		t.Error("a capped line item nowhere near its budget was reported on track")
	}
}

// The projection has to be reachable, and it must never leak in production:
// delivery state says which advertisers are struggling to spend.
func TestDeliveryEndpointIsLabOnly(t *testing.T) {
	prod := testServer()
	w := httptest.NewRecorder()
	prod.routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/delivery", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("production exposed /delivery with %d", w.Code)
	}

	lab := testServer()
	lab.lab = true
	w2 := httptest.NewRecorder()
	lab.routes().ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/delivery", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("lab returned %d for /delivery", w2.Code)
	}

	var body struct {
		LineItems []map[string]any `json:"line_items"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &body); err != nil {
		t.Fatalf("unparseable: %v", err)
	}
	if len(body.LineItems) == 0 {
		t.Fatal("no line items projected")
	}
	for _, li := range body.LineItems {
		if _, ok := li["on_track"]; !ok {
			t.Errorf("row has no on_track: %v", li)
		}
	}
}
