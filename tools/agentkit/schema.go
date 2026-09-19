// Package agentkit is the portability layer for the agent fleet.
//
// Each agent needs surprisingly few facts. Making those facts the interface --
// rather than our field names, our storage and our control plane -- is what
// lets an agent be dropped into another company by writing a mapping file
// instead of forking the tool.
//
// See docs/portable-agents.md.
package agentkit

// EventType is the canonical vocabulary. Deliberately tiny: an agent that needs
// six facts can be integrated by producing six facts, and a large canonical
// schema is a large integration.
type EventType string

const (
	EventRequest    EventType = "request"
	EventImpression EventType = "impression"
	EventClick      EventType = "click"
	// EventEngagement is any signal that a human interacted with the content.
	// Named generically because it is a game start here and a video quartile,
	// scroll depth or article read elsewhere. Two of the Integrity rules depend
	// on it, and it is the fact most often unavailable at another company.
	EventEngagement EventType = "engagement"
	EventOther      EventType = "other"
)

// Event is the canonical form every agent reads. A company's own schema is
// mapped onto this; nothing downstream knows the original field names.
type Event struct {
	Type EventType

	// Counterparty is whoever the cost and revenue attach to on the supply
	// side -- publisher, site, app, tenant. The name is generic because the
	// unit differs per company, and arguing about which one it should be is
	// usually the longest part of a FinOps integration.
	Counterparty string

	BuyerID   string
	SessionID string
	TSMillis  int64
	Country   string

	// Revenue in whole currency units, already scaled. Sources reporting
	// micros, CPM or cents declare a scale in the mapping.
	Revenue float64

	ComputeMS          float64
	DownstreamCalls    int
	DownstreamTimeouts int
	DownstreamBids     int

	// Arms maps experiment key -> variant id. Empty for almost every company,
	// which is exactly why the Yield agent is the hard one to port.
	Arms map[string]string

	// Billable is nil when the source does not express the concept.
	Billable *bool

	// Raw is the original record, so an agent can reach for something the
	// canonical schema does not carry. Using it makes that agent less portable,
	// which is the intended friction.
	Raw map[string]any
}

// Resolution records how a mapping actually bound, so a run can state which
// facts it has rather than silently producing a confident wrong number.
type Resolution struct {
	Resolved  []string // canonical field -> found in the source
	Defaulted []string // not mapped, a default was used
	Missing   []string // mapped, but absent from every record read
}

// Consequence explains, in plain words, what a missing fact does to the output.
// An agent that degrades silently is worse than one that refuses to run.
var Consequence = map[string]string{
	"compute_ms":          "compute cost reads as zero; compute-heavy counterparties will be understated",
	"downstream_calls":    "downstream call cost reads as zero; this is usually the largest single cost line",
	"counterparty":        "everything aggregates into one bucket; per-publisher ranking is impossible",
	"revenue":             "margin cannot be computed; only cost ranking is available",
	"session_id":          "session-level rules are skipped; integrity findings will be incomplete",
	"engagement":          "the impressions-without-engagement rule is skipped",
	"arms":                "the yield agent cannot run at all -- there is nothing to compare",
	"downstream_timeouts": "timeout cost is invisible; a buyer that never answers will look free",
}
