package main

import (
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Frequency capping, and the privacy trade it forces into the open.
//
// A frequency cap limits how often one PERSON sees one thing. That definition
// contains the whole problem: capping requires knowing that two requests came
// from the same person, which is exactly the capability our privacy baseline
// declines to build.
//
// This is the clearest case in the whole project where privacy and advertising
// effectiveness genuinely trade off. Most privacy conflicts in AdTech dissolve
// under examination -- you did not need the identifier, you needed the
// aggregate. This one does not dissolve. A cap is *defined* over a person.
//
// So we build the honest version and state precisely what it cannot do:
//
//   SESSION-SCOPED CAPPING. The cap holds within one browsing session, keyed on
//   a session id that already exists, lives for the tab, and identifies nobody
//   across sessions, devices or days.
//
// What that buys: the common case. "Do not show the same banner five times
// while somebody plays three games" is most of the value of frequency capping,
// and it needs no persistent identifier at all.
//
// What it cannot do, stated plainly rather than glossed:
//   - cap across sessions, days or devices
//   - measure TRUE frequency -- we can only ever report per-session frequency,
//     and reporting that as if it were per-person would be a lie
//   - resist a client that discards its session id to reset the cap
//
// The third is worth dwelling on: a session id is client-asserted, so a
// determined client can reset its cap. That is acceptable while the advertiser
// is us. It stops being acceptable the moment someone pays for a capped
// campaign, and at that point the honest options are a real identifier (a gated
// decision) or not selling capped campaigns. Not "tighten the heuristics".
// ---------------------------------------------------------------------------

// FrequencyState counts impressions per (session, subject). Subject is a line
// item or creative id -- capping "this creative" and "this campaign" are
// different products and the store does not care which.
type FrequencyState interface {
	Count(sessionID, subject string) int
	Increment(sessionID, subject string)
}

// MemoryFrequency is the local/dev implementation. In AWS this is DynamoDB with
// a short TTL, following the same shape as DynamoBudget: an atomic counter and
// a per-invocation cache.
//
// The TTL is the whole storage design. A session-scoped counter has no value
// after the session ends, so rows expire rather than accumulate -- which is
// both a cost property and a data-minimisation one. Two reasons pointing the
// same way is a good sign.
type MemoryFrequency struct {
	mu     sync.RWMutex
	counts map[string]int
	seen   map[string]time.Time
	ttl    time.Duration
	now    func() time.Time
}

func NewMemoryFrequency(ttl time.Duration) *MemoryFrequency {
	return &MemoryFrequency{
		counts: map[string]int{},
		seen:   map[string]time.Time{},
		ttl:    ttl,
		now:    time.Now,
	}
}

func freqKey(sessionID, subject string) string { return sessionID + "\x00" + subject }

func (m *MemoryFrequency) Count(sessionID, subject string) int {
	if sessionID == "" {
		// No session means no way to count, so no cap can apply. Returning 0
		// serves the ad rather than suppressing it: a cap that fires when it
		// cannot measure is a cap that silently kills delivery.
		return 0
	}
	k := freqKey(sessionID, subject)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.seen[k]; ok && m.now().Sub(t) > m.ttl {
		return 0
	}
	return m.counts[k]
}

func (m *MemoryFrequency) Increment(sessionID, subject string) {
	if sessionID == "" {
		return
	}
	k := freqKey(sessionID, subject)
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.seen[k]; ok && m.now().Sub(t) > m.ttl {
		m.counts[k] = 0
	}
	m.counts[k]++
	m.seen[k] = m.now()
}

// FrequencyCap is the cap configured on a line item.
type FrequencyCap struct {
	// Impressions is the maximum within Scope. Zero means uncapped.
	Impressions int `json:"impressions"`
	// Scope is currently only "session". Named rather than assumed so that
	// adding "day" later is a visible decision requiring an identifier, not a
	// config value someone sets without noticing what it implies.
	Scope string `json:"scope"`
}

const ScopeSession = "session"

// Capped reports whether this line item has exhausted its cap for this session.
func (c FrequencyCap) Capped(fs FrequencyState, sessionID, subject string) bool {
	if c.Impressions <= 0 || fs == nil {
		return false
	}
	if c.Scope != "" && c.Scope != ScopeSession {
		// An unimplemented scope must not silently behave like a different one.
		// Serving uncapped is the safe failure: it over-delivers rather than
		// silently suppressing a campaign nobody can see is suppressed.
		return false
	}
	return fs.Count(sessionID, subject) >= c.Impressions
}
