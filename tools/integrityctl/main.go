// integrityctl -- Layer 4 of traffic filtering.
//
// Reviews logged events after the fact and reports which impressions should not
// have been billed, using evidence that did not exist when the serving decision
// was made.
//
//	integrityctl review --events 'events/*.ndjson'
//	integrityctl review --events 'events/*.ndjson' --out revocations.json
//
// Like yieldctl, it reports rather than mutates. Applying a revocation changes
// what an advertiser is charged, and the first version of that must not happen
// as a side effect of a cron job nobody read the output of.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type event struct {
	Event     string  `json:"event"`
	EventID   string  `json:"event_id"`
	SessionID string  `json:"session_id"`
	TS        int64   `json:"ts"`
	Country   string  `json:"country"`
	PriceCPM  float64 `json:"price_cpm"`
	Billable  *bool   `json:"billable"`
	Env       string  `json:"env"`
	ImpEvent  string  `json:"impression_event_id"`
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "review" {
		fmt.Fprintln(os.Stderr, `integrityctl review --events GLOB [--out FILE] [--include-lab]`)
		os.Exit(2)
	}
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	events := fs.String("events", "", "glob of event files (.ndjson or .ndjson.gz)")
	out := fs.String("out", "", "write revocations as JSON here")
	includeLab := fs.Bool("include-lab", false, "include env=lab traffic")
	_ = fs.Parse(os.Args[2:])
	if *events == "" {
		fmt.Fprintln(os.Stderr, "integrityctl: --events is required")
		os.Exit(2)
	}

	sessions, err := load(*events, *includeLab)
	if err != nil {
		fmt.Fprintln(os.Stderr, "integrityctl:", err)
		os.Exit(1)
	}

	th := DefaultThresholds()
	var revs []Revocation
	var totalRev, revokedRev float64
	var totalImps, revokedImps int

	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		s := sessions[id]
		for _, im := range s.Impressions {
			totalRev += im.Revenue
			totalImps++
		}
		if r := Review(*s, th); r != nil {
			revs = append(revs, *r)
			revokedRev += r.Revenue
			revokedImps += len(r.EventIDs)
		}
	}

	byReason := map[string]int{}
	for _, r := range revs {
		for _, reason := range r.Reasons {
			byReason[reason]++
		}
	}

	fmt.Printf("sessions reviewed   %d\n", len(sessions))
	fmt.Printf("impressions         %d\n", totalImps)
	fmt.Printf("revenue             %.4f\n\n", totalRev)
	fmt.Printf("sessions revoked    %d\n", len(revs))
	fmt.Printf("impressions revoked %d", revokedImps)
	if totalImps > 0 {
		fmt.Printf("  (%.2f%%)", float64(revokedImps)/float64(totalImps)*100)
	}
	fmt.Printf("\nrevenue revoked     %.4f", revokedRev)
	if totalRev > 0 {
		fmt.Printf("  (%.2f%%)", revokedRev/totalRev*100)
	}
	fmt.Println()

	if len(byReason) > 0 {
		fmt.Println("\nby reason")
		reasons := make([]string, 0, len(byReason))
		for r := range byReason {
			reasons = append(reasons, r)
		}
		sort.Strings(reasons)
		for _, r := range reasons {
			fmt.Printf("  %-28s %d\n", r, byReason[r])
		}
	}

	// A revocation rate that high is far likelier to be a broken rule than a
	// site that is 40% fraudulent. Say so rather than quietly refunding.
	if totalImps > 0 && float64(revokedImps)/float64(totalImps) > 0.40 {
		fmt.Println("\nWARNING: over 40% of impressions revoked. Check the rules before" +
			" acting on this -- a threshold bug looks exactly like a fraud wave.")
	}

	if *out == "" {
		fmt.Println("\n(report only -- pass --out to write the revocation list)")
		return
	}
	b, _ := json.MarshalIndent(revs, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "integrityctl:", err)
		os.Exit(1)
	}
	fmt.Printf("\nwrote %s (%d revocations)\n", *out, len(revs))
}

func load(glob string, includeLab bool) (map[string]*SessionView, error) {
	files, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files matched %q", glob)
	}

	sessions := map[string]*SessionView{}
	// Clicks are separate events, so impressions are indexed to attach them.
	// Indices rather than pointers: append reallocates the backing array, so a
	// stored *Impression goes stale the moment the slice grows and every later
	// click would attach to a discarded copy.
	type loc struct {
		session string
		idx     int
	}
	impIndex := map[string]loc{}

	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		var sc *bufio.Scanner
		if strings.HasSuffix(f, ".gz") {
			gz, err := gzip.NewReader(bufio.NewReader(fh))
			if err != nil {
				fh.Close()
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			sc = bufio.NewScanner(gz)
		} else {
			sc = bufio.NewScanner(bufio.NewReader(fh))
		}
		sc.Buffer(make([]byte, 1<<20), 1<<20)

		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil || e.SessionID == "" {
				continue
			}
			if !includeLab && e.Env == "lab" {
				continue
			}
			s := sessions[e.SessionID]
			if s == nil {
				s = &SessionView{SessionID: e.SessionID, Env: e.Env}
				sessions[e.SessionID] = s
			}
			if e.Country != "" {
				s.Countries = append(s.Countries, e.Country)
			}
			switch e.Event {
			case "ad_request":
				s.RequestTS = append(s.RequestTS, e.TS)
			case "game_start":
				s.GameStarts++
			case "impression":
				// Only review what was actually billable. Traffic the pre-bid
				// classifier already declined to bill needs no second refusal.
				if e.Billable != nil && !*e.Billable {
					continue
				}
				s.Impressions = append(s.Impressions, Impression{
					EventID: e.EventID, TSMillis: e.TS, Revenue: e.PriceCPM / 1000,
				})
				impIndex[e.EventID] = loc{e.SessionID, len(s.Impressions) - 1}
			case "click":
				if l, ok := impIndex[e.ImpEvent]; ok {
					if owner := sessions[l.session]; owner != nil && l.idx < len(owner.Impressions) {
						owner.Impressions[l.idx].Clicked = true
						owner.Impressions[l.idx].ClickTS = e.TS
					}
				}
			}
		}
		fh.Close()
	}
	return sessions, nil
}
