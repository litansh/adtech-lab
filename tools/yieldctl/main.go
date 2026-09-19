// yieldctl -- the cold-path controller.
//
// Reads logged ad-server events, works out which arm of an experiment is
// actually winning, and proposes new bucket ranges for the control plane.
//
//	yieldctl analyse --events 'events/*.ndjson' --experiment floor_price
//	yieldctl propose --events 'events/*.ndjson' --experiment floor_price \
//	    --control-plane services/ad-server/fixtures/control-plane.json --out new.json
//
// It deliberately does NOT write to DynamoDB. It emits an updated control-plane
// file, which `adlabctl seed` applies. Keeping those two steps separate means
// the first version of an autonomous yield controller cannot move real money
// without a human or an explicit cron step in between -- and when that step is
// removed, removing it is a decision someone made rather than a default.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type event struct {
	Event       string            `json:"event"`
	SessionID   string            `json:"session_id"`
	Experiments map[string]string `json:"experiments"`
	Billable    *bool             `json:"billable"`
	Revenue     float64           `json:"revenue"`
	PriceCPM    float64           `json:"price_cpm"`
	Source      string            `json:"source"`
	Game        string            `json:"game"`
	Env         string            `json:"env"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	events := fs.String("events", "", "glob of event files (.ndjson or .ndjson.gz)")
	shadowPath := fs.String("shadow", "shadow.jsonl", "shadow record file")
	day := fs.String("day", "", "day label for a shadow record (default: today)")
	expKey := fs.String("experiment", "", "experiment key to analyse")
	cpPath := fs.String("control-plane", "services/ad-server/fixtures/control-plane.json", "control plane file")
	outPath := fs.String("out", "", "write the updated control plane here (propose only)")
	iters := fs.Int("bootstrap", 5000, "bootstrap iterations")
	seed := fs.Int64("seed", 1, "rng seed, so a run is reproducible")
	includeLab := fs.Bool("include-lab", false, "include env=lab traffic (never for a real decision)")
	_ = fs.Parse(os.Args[2:])

	if *events == "" || *expKey == "" {
		usage()
	}

	obs, err := load(*events, *expKey, *includeLab)
	if err != nil {
		die(err)
	}
	if len(obs) == 0 {
		die(fmt.Errorf("no observations for experiment %q -- is it assigned and logged?", *expKey))
	}

	cp, err := loadControlPlane(*cpPath)
	if err != nil {
		die(err)
	}
	exp := findExperiment(cp, *expKey)
	if exp == nil {
		die(fmt.Errorf("experiment %q not found in %s", *expKey, *cpPath))
	}

	rng := rand.New(rand.NewSource(*seed))
	stats, ivals := report(obs, exp.ControlID, *iters, rng)

	guards := DefaultGuardrails()
	control := statFor(stats, exp.ControlID)
	breached := map[string][]string{}
	for _, s := range stats {
		if b := guards.Breaches(s, control); len(b) > 0 {
			breached[s.ID] = b
		}
	}

	printReport(*expKey, stats, ivals, breached, exp.ControlID)

	if cmd == "analyse" || cmd == "analyze" {
		return
	}
	if cmd == "evaluate" {
		runEvaluate(*shadowPath)
		return
	}
	if cmd != "propose" && cmd != "shadow" {
		usage()
	}

	holdout := shareOf(exp, exp.ControlID)
	if holdout < 0.05 {
		holdout = 0.05
	}
	currentShares := map[string]float64{}
	for _, v := range exp.Variants {
		currentShares[v.ID] = float64(v.BucketEnd-v.BucketStart) / float64(BucketCount)
	}
	props := Allocate(stats, exp.ControlID, holdout, breached, currentShares, rng)

	fmt.Println("\nProposed allocation")
	for _, p := range props {
		note := ""
		if len(p.Reasons) > 0 {
			note = "  [breached: " + strings.Join(p.Reasons, ",") + "]"
		}
		fmt.Printf("  %-8s %5.1f%% -> %5.1f%%  buckets %5d-%-5d%s\n",
			p.ArmID, p.FromShare*100, p.ToShare*100, p.ToStart, p.ToEnd, note)
	}

	if cmd == "shadow" {
		// Record the decision. Do not apply it. This is the only stage that
		// produces evidence a sceptic can act on, and it produces that evidence
		// precisely BECAUSE nothing was applied.
		rec := ShadowRecord{
			Day: *day, Experiment: *expKey,
			Actual: currentShares, Proposed: map[string]float64{},
			RevPerRequest: map[string]float64{}, Breached: breached,
		}
		if rec.Day == "" {
			rec.Day = "unlabelled"
		}
		for _, p := range props {
			rec.Proposed[p.ArmID] = p.ToShare
		}
		for _, st := range stats {
			rec.Requests += st.Requests
			if st.Requests > 0 {
				rec.RevPerRequest[st.ID] = st.RevenuePer1k / 1000
			}
		}
		if err := AppendShadow(*shadowPath, rec); err != nil {
			die(err)
		}
		fmt.Printf("\nshadow record appended to %s -- NOTHING WAS APPLIED\n", *shadowPath)
		fmt.Println("Run `yieldctl evaluate --shadow " + *shadowPath +
			"` once there are at least 14 days.")
		return
	}

	if *outPath == "" {
		fmt.Println("\n(dry run -- pass --out to write an updated control plane)")
		return
	}
	applyProposal(exp, props)
	if err := writeControlPlane(*outPath, cp); err != nil {
		die(err)
	}
	fmt.Printf("\nwrote %s\napply with: adlabctl seed --file %s\n", *outPath, *outPath)
}

// load reduces a stream of events to one Observation per assignment unit.
func load(glob, expKey string, includeLab bool) ([]Observation, error) {
	files, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files matched %q", glob)
	}

	type acc struct {
		arm      string
		revenue  float64
		requests int
		paid     int
		replayed bool
	}
	units := map[string]*acc{}

	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		var r = bufio.NewReader(fh)
		var sc *bufio.Scanner
		if strings.HasSuffix(f, ".gz") {
			gz, err := gzip.NewReader(r)
			if err != nil {
				fh.Close()
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			sc = bufio.NewScanner(gz)
		} else {
			sc = bufio.NewScanner(r)
		}
		sc.Buffer(make([]byte, 1<<20), 1<<20)

		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue // a malformed line is not worth failing a whole run over
			}
			// Synthetic traffic must never appear in a figure treated as real.
			if !includeLab && e.Env == "lab" {
				continue
			}
			arm, ok := e.Experiments[expKey]
			if !ok || e.SessionID == "" {
				continue
			}
			a := units[e.SessionID]
			if a == nil {
				a = &acc{arm: arm}
				units[e.SessionID] = a
			}
			switch e.Event {
			case "ad_request":
				a.requests++
				// Paid fill, not total fill: the house fallback always fills, so
				// total fill was 100% at every floor in the sweep -- including
				// the one that destroyed 91% of revenue.
				if e.Source != "" && e.Source != "none" && e.Source != "house" {
					a.paid++
				}
			case "impression":
				a.revenue += e.PriceCPM / 1000
				if e.Revenue > 0 {
					a.revenue += e.Revenue - e.PriceCPM/1000
				}
			case "game_replay":
				a.replayed = true
			}
		}
		fh.Close()
	}

	out := make([]Observation, 0, len(units))
	for id, a := range units {
		out = append(out, Observation{
			Unit: id + "|" + a.arm, Revenue: a.revenue,
			Requests: a.requests, Paid: a.paid, Replayed: a.replayed,
		})
	}
	return out, nil
}

func report(obs []Observation, controlID string, iters int, rng *rand.Rand) ([]ArmStats, map[string]Interval) {
	byArm := map[string][]Observation{}
	for _, o := range obs {
		arm := o.Unit[strings.LastIndex(o.Unit, "|")+1:]
		byArm[arm] = append(byArm[arm], o)
	}
	ids := make([]string, 0, len(byArm))
	for id := range byArm {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	stats := make([]ArmStats, 0, len(ids))
	for _, id := range ids {
		stats = append(stats, Summarise(id, byArm[id]))
	}
	ivals := map[string]Interval{}
	for _, id := range ids {
		if id == controlID {
			continue
		}
		ivals[id] = BootstrapDiff(byArm[id], byArm[controlID], iters, rng)
	}
	return stats, ivals
}

func printReport(key string, stats []ArmStats, ivals map[string]Interval,
	breached map[string][]string, controlID string) {

	fmt.Printf("experiment: %s\n\n", key)
	fmt.Printf("  %-8s %8s %9s %11s %9s %9s\n", "arm", "units", "requests", "rev/1k", "paid fill", "replay")
	for _, s := range stats {
		tag := ""
		if s.ID == controlID {
			tag = " (control)"
		}
		fmt.Printf("  %-8s %8d %9d %11.4f %8.1f%% %8.1f%%%s\n",
			s.ID, s.Units, s.Requests, s.RevenuePer1k,
			s.PaidFill*100, s.ReplayRate*100, tag)
	}

	fmt.Println("\n  vs control, revenue per 1000 (bootstrap 95% CI on the difference)")
	for _, s := range stats {
		iv, ok := ivals[s.ID]
		if !ok {
			continue
		}
		verdict := "no measurable difference"
		if iv.Significant && iv.Diff > 0 {
			verdict = "BETTER"
		} else if iv.Significant {
			verdict = "WORSE"
		}
		fmt.Printf("    %-8s %+8.4f  [%+.4f, %+.4f]  %s\n", s.ID, iv.Diff, iv.Low, iv.High, verdict)
	}
	if len(breached) > 0 {
		fmt.Println("\n  guardrail breaches")
		for id, rs := range breached {
			fmt.Printf("    %-8s %s\n", id, strings.Join(rs, ", "))
		}
	}
}

// --- control plane -------------------------------------------------------

type variant struct {
	ID          string            `json:"id"`
	Params      map[string]string `json:"params"`
	BucketStart int               `json:"bucket_start"`
	BucketEnd   int               `json:"bucket_end"`
}

type experiment struct {
	Key       string    `json:"key"`
	Active    bool      `json:"active"`
	Unit      string    `json:"unit"`
	ControlID string    `json:"control_id"`
	Variants  []variant `json:"variants"`
}

func loadControlPlane(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

func findExperiment(cp map[string]any, key string) *experiment {
	raw, ok := cp["experiments"]
	if !ok {
		return nil
	}
	b, _ := json.Marshal(raw)
	var xs []experiment
	if json.Unmarshal(b, &xs) != nil {
		return nil
	}
	for i := range xs {
		if xs[i].Key == key {
			return &xs[i]
		}
	}
	return nil
}

func shareOf(e *experiment, id string) float64 {
	for _, v := range e.Variants {
		if v.ID == id {
			return float64(v.BucketEnd-v.BucketStart) / float64(BucketCount)
		}
	}
	return 0
}

func applyProposal(e *experiment, props []Proposal) {
	by := map[string]Proposal{}
	for _, p := range props {
		by[p.ArmID] = p
	}
	for i := range e.Variants {
		if p, ok := by[e.Variants[i].ID]; ok {
			e.Variants[i].BucketStart = p.ToStart
			e.Variants[i].BucketEnd = p.ToEnd
		}
	}
}

func writeControlPlane(path string, cp map[string]any) error {
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func statFor(stats []ArmStats, id string) ArmStats {
	for _, s := range stats {
		if s.ID == id {
			return s
		}
	}
	return ArmStats{ID: id}
}

func runEvaluate(path string) {
	records, err := LoadShadow(path)
	if err != nil {
		die(err)
	}
	if len(records) == 0 {
		die(fmt.Errorf("no shadow records in %s", path))
	}
	e := Evaluate(records)
	verdict, why := Verdict(e)

	fmt.Printf("shadow evaluation over %d day(s)\n\n", e.Days)
	fmt.Printf("  what actually ran      $%.4f\n", e.ActualUSD)
	fmt.Printf("  what the agent wanted  $%.4f\n", e.ProposedUSD)
	fmt.Printf("  difference             $%+.4f  (%+.2f%%)\n", e.UpliftUSD, e.UpliftPct)
	fmt.Printf("  better on %d days, worse on %d\n", e.DaysBetter, e.DaysWorse)
	if e.WorstDay != "" {
		fmt.Printf("  worst day              %.2f%% (%s)\n", e.WorstDayPct, e.WorstDay)
	}
	fmt.Printf("\n  VERDICT: %s -- %s\n", verdict, why)
	fmt.Println("\n  Counterfactual assumption: an arm's revenue rate does not change with")
	fmt.Println("  the share of traffic it gets. Fair for small share changes, poor for")
	fmt.Println("  large ones -- which is why the controller moves at most 10% per run.")
}

func usage() {
	fmt.Fprintln(os.Stderr, `yieldctl -- cold-path yield controller

  yieldctl analyse  --events GLOB --experiment KEY
  yieldctl propose  --events GLOB --experiment KEY [--out FILE]
  yieldctl shadow   --events GLOB --experiment KEY [--shadow FILE] [--day YYYY-MM-DD]
  yieldctl evaluate --shadow FILE

Flags:
  --events         glob of ad-server event files (.ndjson or .ndjson.gz)
  --experiment     experiment key, e.g. floor_price
  --control-plane  control plane JSON to read (default: fixtures)
  --out            write updated control plane (propose only; dry run without it)
  --bootstrap      bootstrap iterations (default 5000)
  --seed           rng seed, so a run is reproducible (default 1)
  --include-lab    include env=lab traffic -- never for a real decision`)
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "yieldctl:", err)
	os.Exit(1)
}
