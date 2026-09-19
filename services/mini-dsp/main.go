// mini-dsp -- a buyer, as a separate process.
//
//	go run . -addr :8081 -campaigns campaigns.json
//
// Endpoints:
//
//	POST /bid    OpenRTB-ish bid request -> 200 with a bid, or 204 no-bid
//	GET  /win    the seller's win notice (nurl)
//	GET  /stats  what THIS side believes happened
//
// /stats is the interesting one. Its numbers will not match the seller's, and
// docs/reporting-discrepancy.md explains every reason why.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"
)

type server struct {
	bidder *Bidder
	// latency simulates this buyer's own processing time, so the seller's tmax
	// is a real deadline rather than a formality.
	minLatency, maxLatency time.Duration
	// lossRate drops win notices, because the single biggest source of
	// discrepancy in real reporting is notifications that never arrive.
	lossRate float64
	rng      *rand.Rand
	verbose  bool
}

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	campaignsPath := flag.String("campaigns", "campaigns.json", "campaign file")
	minLat := flag.Int("min-latency-ms", 15, "minimum processing time")
	maxLat := flag.Int("max-latency-ms", 60, "maximum processing time")
	lossRate := flag.Float64("nurl-loss", 0.0, "fraction of win notices to drop")
	seed := flag.Int64("seed", 1, "rng seed")
	base := flag.String("base", "", "absolute base URL for win notices (defaults to http://127.0.0.1<addr>)")
	verbose := flag.Bool("v", false, "log every decision")
	flag.Parse()

	raw, err := os.ReadFile(*campaignsPath)
	if err != nil {
		log.Fatalf("campaigns: %v", err)
	}
	var cs []Campaign
	if err := json.Unmarshal(raw, &cs); err != nil {
		log.Fatalf("campaigns: %v", err)
	}
	if len(cs) == 0 {
		log.Fatal("no campaigns")
	}

	baseURL := *base
	if baseURL == "" {
		baseURL = "http://127.0.0.1" + *addr
	}

	s := &server{
		bidder:     NewBidder(cs, *seed, baseURL),
		minLatency: time.Duration(*minLat) * time.Millisecond,
		maxLatency: time.Duration(*maxLat) * time.Millisecond,
		lossRate:   *lossRate,
		rng:        rand.New(rand.NewSource(*seed + 1)),
		verbose:    *verbose,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/bid", s.handleBid)
	mux.HandleFunc("/win", s.handleWin)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("mini-dsp listening on %s with %d campaign(s), win notices to %s",
		*addr, len(cs), baseURL)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func (s *server) handleBid(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req BidRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Our own processing time. The seller may give up before we answer, and
	// when it does we have spent the compute and earned nothing -- which is
	// why a slow bidder is expensive for BOTH sides.
	think := s.minLatency
	if s.maxLatency > s.minLatency {
		think += time.Duration(s.rng.Int63n(int64(s.maxLatency - s.minLatency)))
	}
	time.Sleep(think)

	d := s.bidder.Decide(req)
	if s.verbose {
		if d.Bid != nil {
			log.Printf("bid  %s $%.2f (%s) after %v", req.ID, d.Bid.Price, d.Camp.ID, think)
		} else {
			log.Printf("nobid %s %s after %v", req.ID, d.Reason, think)
		}
	}

	if d.Bid == nil {
		// 204 is the OpenRTB no-bid. A JSON body saying "no" costs bandwidth on
		// the majority of requests, since most requests are no-bids.
		w.Header().Set("X-Nobid-Reason", d.Reason)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp := BidResponse{
		ID:  req.ID,
		Cur: "USD",
		SeatBid: []SeatBid{{
			Seat: "mini-dsp",
			Bid:  []Bid{*d.Bid},
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *server) handleWin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cid := q.Get("cid")
	if cid == "" {
		http.Error(w, "missing cid", http.StatusBadRequest)
		return
	}

	// Deliberately lose some notices. This is not a fault injection toggle for
	// testing -- it is a model of reality. Win notices are fire-and-forget GETs
	// from a browser or a server that is already moving on, and a meaningful
	// fraction never arrive. Everything the buyer believes about delivery flows
	// through this endpoint, so a lost notice is a permanent under-count that
	// nobody will ever reconcile.
	if s.lossRate > 0 && s.rng.Float64() < s.lossRate {
		if s.verbose {
			log.Printf("win  %s DROPPED (simulated notice loss)", cid)
		}
		w.WriteHeader(http.StatusOK) // the seller sees success either way
		return
	}

	price, _ := strconv.ParseFloat(q.Get("price"), 64)
	s.bidder.NotifyWin(cid, q.Get("user"), price)
	if s.verbose {
		log.Printf("win  %s $%.2f", cid, price)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"source": "mini-dsp",
		"note": "These are the BUYER's numbers. They will not match the seller's. " +
			"See docs/reporting-discrepancy.md.",
		"campaigns": s.bidder.Stats(),
	})
}
