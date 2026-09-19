package agentkit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustMapping(t *testing.T, body string) *Mapping {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := LoadMapping(p)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The portability claim in one test: the SAME agent code reads two completely
// different schemas, and only the mapping file differs.
func TestTwoDifferentSchemasProduceIdenticalCanonicalEvents(t *testing.T) {
	ours := mustMapping(t, `{
      "fields": {"event_type":"event","counterparty":"publisher_id",
                 "revenue":"price_cpm","compute_ms":"latency_ms",
                 "downstream_calls":"auction_buyers_called"},
      "scales": {"revenue": 0.001},
      "event_values": {"impression":"impression"}}`)

	// A different company: nested, revenue in micros, different names.
	theirs := mustMapping(t, `{
      "fields": {"event_type":"type","counterparty":"supply.partner_id",
                 "revenue":"economics.gross_micros","compute_ms":"timing.handler_ms",
                 "downstream_calls":"auction.partners_queried"},
      "scales": {"revenue": 0.000001},
      "event_values": {"impression":"IMP"}}`)

	ourRec := map[string]any{
		"event": "impression", "publisher_id": "pub-1",
		"price_cpm": 2.50, "latency_ms": 12.0, "auction_buyers_called": 5.0,
	}
	theirRec := map[string]any{
		"type":      "IMP",
		"supply":    map[string]any{"partner_id": "pub-1"},
		"economics": map[string]any{"gross_micros": 2500.0},
		"timing":    map[string]any{"handler_ms": 12.0},
		"auction":   map[string]any{"partners_queried": 5.0},
	}

	a, ok1 := NewReader(ours).Convert(ourRec)
	b, ok2 := NewReader(theirs).Convert(theirRec)
	if !ok1 || !ok2 {
		t.Fatal("a record was filtered out unexpectedly")
	}
	if a.Type != b.Type || a.Counterparty != b.Counterparty ||
		a.ComputeMS != b.ComputeMS || a.DownstreamCalls != b.DownstreamCalls {
		t.Fatalf("schemas did not converge:\n  %+v\n  %+v", a, b)
	}
	if a.Revenue != b.Revenue {
		t.Fatalf("revenue scaling disagreed: %.9f vs %.9f", a.Revenue, b.Revenue)
	}
	if a.Revenue != 0.0025 {
		t.Errorf("revenue %.9f, want 0.0025", a.Revenue)
	}
}

// Getting revenue scale wrong by 1000x is the likeliest integration error, so
// the scale is explicit and applied, never inferred.
func TestRevenueScaleIsApplied(t *testing.T) {
	m := mustMapping(t, `{"fields":{"revenue":"micros"},"scales":{"revenue":0.000001}}`)
	e, _ := NewReader(m).Convert(map[string]any{"micros": 1500000.0})
	if e.Revenue != 1.5 {
		t.Fatalf("revenue %.6f, want 1.5", e.Revenue)
	}
}

// A missing fact must be reported with its consequence, not silently zeroed.
func TestMissingFactsAreReportedWithConsequences(t *testing.T) {
	m := mustMapping(t, `{"fields":{"counterparty":"pub","compute_ms":"never_present"}}`)
	r := NewReader(m)
	r.Convert(map[string]any{"pub": "p1"})

	res := r.Resolution()
	if !contains(res.Resolved, "counterparty") {
		t.Errorf("counterparty should have resolved: %v", res)
	}
	if !contains(res.Missing, "compute_ms") {
		t.Errorf("compute_ms was mapped but absent, should be MISSING: %v", res)
	}
	if !contains(res.Defaulted, "downstream_calls") {
		t.Errorf("downstream_calls was never mapped, should be defaulted: %v", res)
	}

	var sb strings.Builder
	r.PrintResolution(&sb)
	out := sb.String()
	if !strings.Contains(out, "compute cost reads as zero") {
		t.Errorf("consequence of missing compute_ms not stated:\n%s", out)
	}
	if !strings.Contains(out, "largest single cost line") {
		t.Errorf("consequence of unmapped downstream_calls not stated:\n%s", out)
	}
}

func TestFiltersDropRecords(t *testing.T) {
	m := mustMapping(t, `{"fields":{"counterparty":"pub"},"filters":{"env":"production"}}`)
	r := NewReader(m)
	if _, ok := r.Convert(map[string]any{"pub": "p", "env": "lab"}); ok {
		t.Error("lab traffic was not filtered out")
	}
	if _, ok := r.Convert(map[string]any{"pub": "p", "env": "production"}); !ok {
		t.Error("production traffic was filtered out")
	}
	if r.Filtered != 1 || r.Read != 1 {
		t.Errorf("filtered=%d read=%d, want 1 and 1", r.Filtered, r.Read)
	}
}

// Absent a filter field entirely, the record is dropped rather than kept: a
// filter that silently stops applying is how synthetic traffic reaches a
// figure reported as real.
func TestMissingFilterFieldDropsTheRecord(t *testing.T) {
	m := mustMapping(t, `{"fields":{"counterparty":"pub"},"filters":{"env":"production"}}`)
	if _, ok := NewReader(m).Convert(map[string]any{"pub": "p"}); ok {
		t.Fatal("a record with no env field was kept by an env filter")
	}
}

func TestArmsAreExtracted(t *testing.T) {
	m := mustMapping(t, `{"fields":{"counterparty":"pub"},"arms_field":"experiments"}`)
	e, _ := NewReader(m).Convert(map[string]any{
		"pub": "p", "experiments": map[string]any{"floor_price": "f100"}})
	if e.Arms["floor_price"] != "f100" {
		t.Fatalf("arms not extracted: %+v", e.Arms)
	}
}

func TestNoArmsIsReportedAsTheYieldBlocker(t *testing.T) {
	m := mustMapping(t, `{"fields":{"counterparty":"pub"}}`)
	r := NewReader(m)
	r.Convert(map[string]any{"pub": "p"})
	var sb strings.Builder
	r.PrintResolution(&sb)
	if !strings.Contains(sb.String(), "yield agent cannot run") {
		t.Fatalf("absence of arms must state that Yield cannot run:\n%s", sb.String())
	}
}

// --- the ladder ---

func TestObserveRecommendAndShadowCannotWrite(t *testing.T) {
	for _, s := range []Stage{StageObserve, StageRecommend, StageShadow} {
		if s.CanWrite() {
			t.Errorf("%s reports that it can write", s)
		}
		err := s.GuardWrite("update the control plane")
		if err == nil {
			t.Fatalf("%s did not refuse a write", s)
		}
		if !strings.Contains(err.Error(), "must not write") {
			t.Errorf("%s gave an unhelpful refusal: %v", s, err)
		}
	}
}

func TestActingStagesMayWrite(t *testing.T) {
	for _, s := range []Stage{StageBoundedAct, StageAct} {
		if !s.CanWrite() {
			t.Errorf("%s cannot write", s)
		}
		if err := s.GuardWrite("apply"); err != nil {
			t.Errorf("%s refused a write: %v", s, err)
		}
	}
}

func TestStageRoundTrips(t *testing.T) {
	for _, name := range []string{"observe", "recommend", "shadow", "bounded-act", "act"} {
		s, err := ParseStage(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s.String() != name {
			t.Errorf("%s round-tripped to %s", name, s)
		}
	}
	if _, err := ParseStage("yolo"); err == nil {
		t.Error("an unknown stage was accepted")
	}
}

func TestReadNDJSONSkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.ndjson")
	good, _ := json.Marshal(map[string]any{"event": "impression", "pub": "p1"})
	body := string(good) + "\n{not json\n" + string(good) + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m := mustMapping(t, `{"fields":{"event_type":"event","counterparty":"pub"},
                          "event_values":{"impression":"impression"}}`)
	r := NewReader(m)
	n := 0
	if err := r.ReadNDJSON(filepath.Join(dir, "*.ndjson"), func(Event) { n++ }); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("read %d events, want 2 (one malformed line skipped)", n)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
