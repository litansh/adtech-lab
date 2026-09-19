package main

import (
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"sync"
	"testing"
	"time"
)

// Regression test. The first DynamoDB budget implementation double-unlocked an
// RWMutex on the cache-miss-inside-fresh-window path, which killed the Lambda
// on its first request with "RUnlock of unlocked RWMutex". The decision tests
// all used MemoryBudget, so nothing caught it until it reached AWS.
//
// A nil DynamoDB client is fine here: the point is to exercise the locking, and
// any code path that reaches the client would panic loudly rather than pass.
func TestDynamoBudgetCacheLockingDoesNotPanic(t *testing.T) {
	d := &DynamoBudget{cache: map[string]float64{}, cachedAt: time.Now(), ttl: time.Hour}

	// Populate, so subsequent reads are cache hits and never touch the client.
	d.mu.Lock()
	d.cache["li-1"] = 1.5
	d.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, ok := d.cachedSpend("li-1")
			if !ok || v != 1.5 {
				t.Errorf("cachedSpend = %v, %v; want 1.5, true", v, ok)
			}
		}()
	}
	wg.Wait()
}

// Cache expiry must reset cleanly and report a miss, without leaving the mutex
// in a bad state.
func TestDynamoBudgetCacheExpiryResets(t *testing.T) {
	d := &DynamoBudget{cache: map[string]float64{"li-1": 9.9}, cachedAt: time.Now().Add(-time.Hour), ttl: time.Second}

	v, ok := d.cachedSpend("li-1")
	if ok {
		t.Errorf("expired cache reported a hit (%v); want a miss", v)
	}

	// And the mutex must still be usable afterwards.
	d.mu.Lock()
	n := len(d.cache)
	d.mu.Unlock()
	if n != 0 {
		t.Errorf("cache not cleared on expiry: %d entries", n)
	}
}

// The bug this exists for: BUYER and EXPERIMENT were missing from the loader
// AND from adlabctl's seed list, so production could not run an auction at
// all. Nothing failed, because "no buyer bid" and "no buyer was asked" produce
// the same empty auction.
//
// This asserts every kind the loader claims to know is actually routed, so a
// new kind added to the fixture and forgotten here is a test failure rather
// than a silent capability gap.
func TestEveryControlPlaneKindIsRouted(t *testing.T) {
	pass := func(map[string]ddbtypes.AttributeValue, any) error { return nil }
	for _, kind := range ControlPlaneKinds {
		cp := &ControlPlane{}
		if !applyItem(cp, kind, map[string]ddbtypes.AttributeValue{}, pass) {
			t.Errorf("kind %q is listed in ControlPlaneKinds but not handled by applyItem", kind)
		}
	}
	if applyItem(&ControlPlane{}, "NOT_A_KIND", nil, pass) {
		t.Error("an unknown kind was reported as handled")
	}
}

// Every collection the fixture populates must have a kind that reaches
// production. A fixture that carries buyers into a control plane that cannot
// store them is a configuration that only works locally -- which is exactly
// what shipped.
func TestFixtureCollectionsAllReachProduction(t *testing.T) {
	cp, err := loadControlPlane("fixtures/control-plane.json")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	populated := map[string]bool{
		"PUBLISHER":  len(cp.Publishers) > 0,
		"SITE":       len(cp.Sites) > 0,
		"PLACEMENT":  len(cp.Placements) > 0,
		"ADVERTISER": len(cp.Advertisers) > 0,
		"CAMPAIGN":   len(cp.Campaigns) > 0,
		"LINE_ITEM":  len(cp.LineItems) > 0,
		"CREATIVE":   len(cp.Creatives) > 0,
		"BUYER":      len(cp.Buyers) > 0,
		"EXPERIMENT": len(cp.Experiments) > 0,
	}
	known := map[string]bool{}
	for _, k := range ControlPlaneKinds {
		known[k] = true
	}
	for kind, has := range populated {
		if has && !known[kind] {
			t.Errorf("the fixture populates %s but the loader cannot read it: "+
				"this configuration works locally and silently does nothing in production", kind)
		}
	}
	// And the two that were missing must genuinely be there now.
	if len(cp.Buyers) == 0 {
		t.Error("no buyers in the fixture: production would have no auction")
	}
	if len(cp.Experiments) == 0 {
		t.Error("no experiments in the fixture")
	}
}
