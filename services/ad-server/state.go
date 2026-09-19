package main

import "sync"

// ---------------------------------------------------------------------------
// Two different kinds of "spend", with different consistency requirements.
//
//   serving_budget_state   delivery control. Approximate is acceptable.
//                          Bounded over-delivery is a normal, contracted-for
//                          property of real ad servers, because many serving
//                          nodes count concurrently and reconcile later.
//
//   billing_ledger         money. Durable, idempotent, reconcilable. Derived
//                          from the event stream, never from these counters.
//
// Teaching them as one thing would be the most misleading simplification we
// could make. See docs/architecture-proposal.md.
// ---------------------------------------------------------------------------

type BudgetState interface {
	SpendToday(lineItemID string) float64
	AddSpend(lineItemID string, usd float64)
}

// MemoryBudget is the local/dev implementation. In AWS this is backed by
// DynamoDB atomic counters, optionally batched in Lambda memory and flushed --
// which trades DynamoDB write cost against a little more over-delivery.
type MemoryBudget struct {
	mu    sync.RWMutex
	spend map[string]float64
}

func NewMemoryBudget() *MemoryBudget {
	return &MemoryBudget{spend: map[string]float64{}}
}

func (m *MemoryBudget) SpendToday(id string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.spend[id]
}

func (m *MemoryBudget) AddSpend(id string, usd float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spend[id] += usd
}
