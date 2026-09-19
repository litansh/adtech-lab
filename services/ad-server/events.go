package main

import (
	"encoding/json"
	"os"
	"sync"
)

// ---------------------------------------------------------------------------
// Data plane. Append-only, written constantly, read in batch.
//
// Every event carries a unique event_id so replays are idempotent: this is what
// makes the billing ledger reconcilable rather than merely plausible.
//
// Locally these go to stdout as NDJSON. In AWS: Firehose -> S3 -> Athena.
// At 50B events/day the industry uses Kafka plus a stream processor; at our
// scale, Firehose. See docs/architecture-proposal.md.
// ---------------------------------------------------------------------------

type EventSink interface{ Emit(map[string]any) }

type StdoutSink struct{ mu sync.Mutex }

func (s *StdoutSink) Emit(e map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(e)
	if err != nil {
		return // telemetry must never break serving
	}
	os.Stdout.Write(append(b, '\n'))
}
