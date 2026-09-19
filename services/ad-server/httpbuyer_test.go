package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func dspStub(t *testing.T, delay time.Duration, status int, body string) (Buyer, func()) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		if status == http.StatusNoContent {
			w.Header().Set("X-Nobid-Reason", "below_floor")
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	return Buyer{ID: "dsp", Name: "dsp", Endpoint: s.URL}, s.Close
}

const okBid = `{"id":"r1","cur":"USD","seatbid":[{"seat":"s","bid":[
  {"id":"b1","impid":"1","price":3.25,"adm":"<div>ad</div>","crid":"cr1",
   "nurl":"http://example.invalid/win?price=${AUCTION_PRICE}"}]}]}`

func TestRealBuyerBidIsParsed(t *testing.T) {
	b, stop := dspStub(t, 0, 200, okBid)
	defer stop()
	res := callBuyers(context.Background(), []Buyer{b}, ortbRequest{ID: "r1"}, 200)
	if len(res) != 1 {
		t.Fatalf("got %d results", len(res))
	}
	if res[0].Price != 3.25 || res[0].CreativeID != "cr1" || res[0].AdM == "" {
		t.Fatalf("bad parse: %+v", res[0])
	}
	if res[0].NURL == "" {
		t.Error("no win notice url; the buyer can never learn it won")
	}
}

// The bug this test exists for: latency was assigned in a deferred function on
// an UNNAMED return value, so `return res` copied the struct first and every
// buyer reported 0ms. A latency dashboard reading zero everywhere looks fine.
func TestLatencyIsActuallyRecorded(t *testing.T) {
	b, stop := dspStub(t, 40*time.Millisecond, 200, okBid)
	defer stop()
	res := callBuyers(context.Background(), []Buyer{b}, ortbRequest{ID: "r1"}, 500)
	if res[0].LatencyMS < 30 {
		t.Fatalf("latency %dms for a 40ms buyer -- it is not being recorded", res[0].LatencyMS)
	}
}

// tmax is a deadline, not a suggestion.
func TestSlowBuyerIsCutOffAtTmax(t *testing.T) {
	b, stop := dspStub(t, 300*time.Millisecond, 200, okBid)
	defer stop()
	start := time.Now()
	res := callBuyers(context.Background(), []Buyer{b}, ortbRequest{ID: "r1"}, 60)
	took := time.Since(start)

	if !res[0].TimedOut {
		t.Fatalf("a 300ms buyer at a 60ms tmax was not marked timed out: %+v", res[0])
	}
	if took > 200*time.Millisecond {
		t.Fatalf("the auction waited %v for a 60ms deadline", took)
	}
}

// A timeout and a connection failure are different facts: one buyer was too
// slow, the other was not there. Merging them hides which one you have.
func TestTimeoutAndFailureAreDistinguished(t *testing.T) {
	dead := Buyer{ID: "dead", Endpoint: "http://127.0.0.1:1/bid"}
	res := callBuyers(context.Background(), []Buyer{dead}, ortbRequest{ID: "r1"}, 200)
	if res[0].TimedOut {
		t.Error("a refused connection was reported as a timeout")
	}
	if res[0].Err == "" {
		t.Error("a refused connection recorded no error")
	}
}

// One broken buyer must not take the auction down with it.
func TestBuyersAreCalledInParallelAndFailuresAreIsolated(t *testing.T) {
	slow, stopSlow := dspStub(t, 80*time.Millisecond, 200, okBid)
	defer stopSlow()
	broken, stopBroken := dspStub(t, 0, 500, "")
	defer stopBroken()
	garbage, stopGarbage := dspStub(t, 0, 200, "{not json")
	defer stopGarbage()
	good, stopGood := dspStub(t, 80*time.Millisecond, 200, okBid)
	defer stopGood()

	slow.ID, broken.ID, garbage.ID, good.ID = "slow", "broken", "garbage", "good"
	start := time.Now()
	res := callBuyers(context.Background(),
		[]Buyer{slow, broken, garbage, good}, ortbRequest{ID: "r1"}, 400)
	took := time.Since(start)

	if len(res) != 4 {
		t.Fatalf("got %d results, want 4", len(res))
	}
	// Two 80ms buyers in parallel take ~80ms, not ~160ms.
	if took > 200*time.Millisecond {
		t.Errorf("four buyers took %v -- they are being called sequentially", took)
	}
	bids := 0
	for _, r := range res {
		if r.Price > 0 {
			bids++
		}
	}
	if bids != 2 {
		t.Errorf("got %d bids, want 2 (the broken and garbage buyers must not bid)", bids)
	}
}

func TestNoBidReasonIsCarriedBack(t *testing.T) {
	b, stop := dspStub(t, 0, http.StatusNoContent, "")
	defer stop()
	res := callBuyers(context.Background(), []Buyer{b}, ortbRequest{ID: "r1"}, 200)
	if res[0].Price != 0 || res[0].NoBidReason != "below_floor" {
		t.Fatalf("no-bid reason lost: %+v", res[0])
	}
}

// Protected fields are unconditional: there is no field set that omits them.
func TestProtectedFieldsAreAlwaysInTheRequest(t *testing.T) {
	pl := &Placement{ID: "p1", SiteID: "s1", Width: 300, Height: 250}
	// An empty-but-non-nil FieldSet means "omit every optional field".
	empty := FieldSet{}
	r := buildRequest("req1", AdRequest{Country: "IL", SessionID: "s"}, pl, 1.5, 100, empty)

	if r.Source == nil || r.Source.SChain == nil || len(r.Source.SChain.Nodes) == 0 {
		t.Fatal("schain missing; supply chain transparency is not optional")
	}
	if r.Imp[0].Secure != 1 {
		t.Error("secure flag missing; misreporting it misrepresents the inventory")
	}
	if r.Regs == nil {
		t.Error("regs object missing")
	}
	// And the optional ones really are omitted when the field set says so.
	if r.Imp[0].BidFloor != 0 {
		t.Errorf("bidfloor sent despite being omitted: %.2f", r.Imp[0].BidFloor)
	}
	if r.Device != nil && r.Device.UA != "" {
		t.Error("device.ua sent despite being omitted")
	}
}

func TestNurlPriceMacroIsSubstituted(t *testing.T) {
	got := make(chan string, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.RawQuery
		w.WriteHeader(200)
	}))
	defer s.Close()

	notifyWin(s.URL+"/win?price=${AUCTION_PRICE}", 4.5)
	select {
	case q := <-got:
		if strings.Contains(q, "AUCTION_PRICE") {
			t.Fatalf("macro not substituted: %s", q)
		}
		if !strings.Contains(q, "4.5") {
			t.Errorf("clearing price missing from the win notice: %s", q)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("win notice never arrived")
	}
}
