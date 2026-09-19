package agentkit

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Mapping is the "tiny integration": a JSON file naming which of a company's
// fields mean what. Integrating at another company edits this file and nothing
// else -- no fork, no code change, no rebuild.
type Mapping struct {
	// Fields maps a canonical name to a source field. Dotted paths are
	// supported ("auction.buyers_called") because real schemas are nested.
	Fields map[string]string `json:"fields"`

	// EventValues maps a canonical event type to the source's own value for it.
	EventValues map[string]string `json:"event_values"`

	// Scales multiply a numeric field into canonical units. Revenue arrives as
	// micros, cents or CPM depending on the company, and getting this wrong by
	// 1000x is the single most likely integration error -- so it is explicit
	// rather than inferred.
	Scales map[string]float64 `json:"scales"`

	// Filters drop records whose field does not equal the value. Used to keep
	// test or synthetic traffic out of a figure treated as real.
	Filters map[string]string `json:"filters"`

	// ArmsField is the field holding experiment assignments, as an object of
	// key -> variant. Empty at almost every company.
	ArmsField string `json:"arms_field"`
}

// LoadMapping reads a mapping file. A missing file is an error rather than a
// default, because a silently-defaulted mapping produces plausible numbers from
// the wrong columns.
func LoadMapping(path string) (*Mapping, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mapping: %w", err)
	}
	var m Mapping
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("mapping %s: %w", path, err)
	}
	if m.Fields == nil {
		return nil, fmt.Errorf("mapping %s: no \"fields\" object", path)
	}
	return &m, nil
}

// dig walks a dotted path through nested maps.
func dig(rec map[string]any, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	cur := any(rec)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// Reader turns a source into canonical Events, tracking how the mapping bound.
type Reader struct {
	m        *Mapping
	seen     map[string]bool // canonical fields that produced a value at least once
	mapped   map[string]bool // canonical fields the mapping named
	Filtered int
	Read     int
}

func NewReader(m *Mapping) *Reader {
	mapped := map[string]bool{}
	for k, v := range m.Fields {
		if v != "" {
			mapped[k] = true
		}
	}
	if m.ArmsField != "" {
		mapped["arms"] = true
	}
	return &Reader{m: m, seen: map[string]bool{}, mapped: mapped}
}

// Resolution reports which facts this run actually has. Emitted every run, not
// on request: an agent that degrades silently is worse than one that refuses.
func (r *Reader) Resolution() Resolution {
	var res Resolution
	for k := range r.mapped {
		if r.seen[k] {
			res.Resolved = append(res.Resolved, k)
		} else {
			res.Missing = append(res.Missing, k)
		}
	}
	for _, k := range []string{"counterparty", "revenue", "compute_ms",
		"downstream_calls", "downstream_timeouts", "session_id", "buyer_id", "arms"} {
		if !r.mapped[k] {
			res.Defaulted = append(res.Defaulted, k)
		}
	}
	sort.Strings(res.Resolved)
	sort.Strings(res.Missing)
	sort.Strings(res.Defaulted)
	return res
}

func (r *Reader) field(rec map[string]any, canonical string) (any, bool) {
	src, ok := r.m.Fields[canonical]
	if !ok || src == "" {
		return nil, false
	}
	v, found := dig(rec, src)
	if found && v != nil {
		r.seen[canonical] = true
	}
	return v, found
}

func (r *Reader) num(rec map[string]any, canonical string) float64 {
	v, ok := r.field(rec, canonical)
	if !ok {
		return 0
	}
	f, ok := toFloat(v)
	if !ok {
		return 0
	}
	if s, has := r.m.Scales[canonical]; has {
		f *= s
	}
	return f
}

func (r *Reader) str(rec map[string]any, canonical string) string {
	v, ok := r.field(rec, canonical)
	if !ok {
		return ""
	}
	return toString(v)
}

// Convert maps one source record to a canonical Event. Returns false when a
// filter rejects it.
func (r *Reader) Convert(rec map[string]any) (Event, bool) {
	for f, want := range r.m.Filters {
		if got, ok := dig(rec, f); !ok || toString(got) != want {
			r.Filtered++
			return Event{}, false
		}
	}
	r.Read++

	e := Event{
		Counterparty:       r.str(rec, "counterparty"),
		BuyerID:            r.str(rec, "buyer_id"),
		SessionID:          r.str(rec, "session_id"),
		Country:            r.str(rec, "country"),
		TSMillis:           int64(r.num(rec, "ts")),
		Revenue:            r.num(rec, "revenue"),
		ComputeMS:          r.num(rec, "compute_ms"),
		DownstreamCalls:    int(r.num(rec, "downstream_calls")),
		DownstreamTimeouts: int(r.num(rec, "downstream_timeouts")),
		DownstreamBids:     int(r.num(rec, "downstream_bids")),
		Raw:                rec,
	}

	if v, ok := r.field(rec, "billable"); ok {
		if f, ok := toFloat(v); ok {
			b := f != 0
			e.Billable = &b
		}
	}

	if r.m.ArmsField != "" {
		if v, ok := dig(rec, r.m.ArmsField); ok {
			if am, ok := v.(map[string]any); ok && len(am) > 0 {
				e.Arms = map[string]string{}
				for k, av := range am {
					e.Arms[k] = toString(av)
				}
				r.seen["arms"] = true
			}
		}
	}

	// Event type: the source's own value, translated through EventValues.
	raw := r.str(rec, "event_type")
	e.Type = EventOther
	for canonical, sourceValue := range r.m.EventValues {
		if raw == sourceValue {
			e.Type = EventType(canonical)
			break
		}
	}
	return e, true
}

// ReadNDJSON reads newline-delimited JSON, gzipped or not, from a glob.
// A malformed line is skipped rather than failing a whole run -- one bad record
// in a day of logs should not cost the report.
func (r *Reader) ReadNDJSON(glob string, fn func(Event)) error {
	files, err := filepath.Glob(glob)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no files matched %q", glob)
	}
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return err
		}
		var sc *bufio.Scanner
		if strings.HasSuffix(f, ".gz") {
			gz, gerr := gzip.NewReader(bufio.NewReader(fh))
			if gerr != nil {
				fh.Close()
				return fmt.Errorf("%s: %w", f, gerr)
			}
			sc = bufio.NewScanner(gz)
		} else {
			sc = bufio.NewScanner(bufio.NewReader(fh))
		}
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var rec map[string]any
			if json.Unmarshal(sc.Bytes(), &rec) != nil {
				continue
			}
			if e, ok := r.Convert(rec); ok {
				fn(e)
			}
		}
		fh.Close()
	}
	return nil
}

// PrintResolution writes the binding report. Called every run.
func (r *Reader) PrintResolution(out *strings.Builder) {
	res := r.Resolution()
	fmt.Fprintf(out, "read %d records (%d filtered out)\n", r.Read, r.Filtered)
	if len(res.Resolved) > 0 {
		fmt.Fprintf(out, "  resolved:  %s\n", strings.Join(res.Resolved, ", "))
	}
	for _, k := range res.Missing {
		fmt.Fprintf(out, "  MISSING:   %-20s mapped but never present", k)
		if c, ok := Consequence[k]; ok {
			fmt.Fprintf(out, " -- %s", c)
		}
		fmt.Fprintln(out)
	}
	for _, k := range res.Defaulted {
		fmt.Fprintf(out, "  not mapped: %-19s", k)
		if c, ok := Consequence[k]; ok {
			fmt.Fprintf(out, " %s", c)
		}
		fmt.Fprintln(out)
	}
}
