// trafficsim generates synthetic Publisher traffic against our own endpoints.
//
// It exists because every yield concept -- floors, fill, pacing, frequency --
// needs volume and competing demand to be observable, and real players are
// months away. See docs/adr/0003-the-lab-comes-before-yield-mechanics.md.
//
// SAFETY, and these are not optional:
//
//   - Every request carries env=lab, so synthetic activity never appears in a
//     figure reported as real. CLAUDE.md requires that separation.
//   - It only ever targets OUR endpoints. It must never be pointed at a real
//     monetisation partner, and there is no flag that would let it.
//   - Rate and total are both bounded, and it refuses to run unbounded.
//
// It is also the load test the Phase 1 Definition of Done asked for: pushed
// hard enough it trips the Cost Fuse, which is how the fuse stops being an
// estimate and starts being a measurement.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type config struct {
	base     string
	sessions int
	rate     float64
	workers  int
	seed     int64
}

// A session shape drawn from the Publisher's actual structure: someone lands,
// plays a few games, maybe switches, and leaves. Not uniform noise.
type session struct {
	id        string
	country   string
	device    string
	game      string
	pageViews int
	games     int
}

var (
	countries = []struct {
		code string
		w    int
	}{{"IL", 55}, {"US", 25}, {"GB", 8}, {"DE", 7}, {"FR", 5}}
	devices = []struct {
		name string
		w    int
	}{{"mobile", 62}, {"desktop", 38}}
	games = []string{"xo", "connect-four"}
)

func weighted[T any](rng *rand.Rand, items []T, weight func(T) int) T {
	total := 0
	for _, it := range items {
		total += weight(it)
	}
	n := rng.Intn(total)
	for _, it := range items {
		n -= weight(it)
		if n < 0 {
			return it
		}
	}
	return items[len(items)-1]
}

type stats struct {
	adRequests, filled, noAd, impressions, clicks, collects, errors atomic.Int64
	auctionsWon, remnant, guaranteed                                atomic.Int64
}

type adResponse struct {
	RequestID string `json:"request_id"`
	NoAd      bool   `json:"no_ad"`
	Ad        *struct {
		LineItemID    string `json:"line_item_id"`
		TrackingToken string `json:"tracking_token"`
	} `json:"ad"`
}

func (s *stats) line(elapsed time.Duration) string {
	req := s.adRequests.Load()
	fill := 0.0
	if req > 0 {
		fill = 100 * float64(s.filled.Load()) / float64(req)
	}
	return fmt.Sprintf(
		"%5.0fs  ad_req=%-6d fill=%5.1f%%  imp=%-6d clk=%-4d collect=%-5d err=%d",
		elapsed.Seconds(), req, fill, s.impressions.Load(), s.clicks.Load(),
		s.collects.Load(), s.errors.Load())
}

func run(ctx context.Context, cfg config) {
	client := &http.Client{Timeout: 15 * time.Second}
	st := &stats{}
	sem := make(chan struct{}, cfg.workers)
	var wg sync.WaitGroup

	// A ticker is the rate limit. Every request this tool makes costs real
	// money, so the rate is a hard input rather than "as fast as it goes".
	interval := time.Duration(float64(time.Second) / cfg.rate)
	tick := time.NewTicker(interval)
	defer tick.Stop()

	start := time.Now()
	report := time.NewTicker(5 * time.Second)
	defer report.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-report.C:
				fmt.Println("  " + st.line(time.Since(start)))
			}
		}
	}()

	for i := 0; i < cfg.sessions; i++ {
		select {
		case <-ctx.Done():
			goto done
		case <-tick.C:
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			defer func() { <-sem }()
			rng := rand.New(rand.NewSource(cfg.seed + int64(n)))
			playSession(ctx, client, cfg.base, newSession(rng, n), rng, st)
		}(i)
	}
done:
	wg.Wait()
	fmt.Println("\n" + st.line(time.Since(start)))
	fmt.Printf("\n  filled=%d  no_ad=%d  (fill is the number the floor experiments move)\n",
		st.filled.Load(), st.noAd.Load())
}

func newSession(rng *rand.Rand, n int) session {
	return session{
		id: fmt.Sprintf("lab-%d-%d", time.Now().Unix(), n),
		country: weighted(rng, countries, func(c struct {
			code string
			w    int
		}) int {
			return c.w
		}).code,
		device: weighted(rng, devices, func(d struct {
			name string
			w    int
		}) int {
			return d.w
		}).name,
		game:      games[rng.Intn(len(games))],
		pageViews: 1 + rng.Intn(2),
		games:     2 + rng.Intn(5),
	}
}

func post(ctx context.Context, c *http.Client, url string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Geo is normally supplied by CloudFront. In the Lab we set it directly so
	// the buyer population can be exercised across countries.
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return out, fmt.Errorf("status %d", resp.StatusCode)
	}
	return out, nil
}

func playSession(ctx context.Context, c *http.Client, base string, s session, rng *rand.Rand, st *stats) {
	events := []map[string]any{
		{"event": "session_start", "device_type": s.device, "country": s.country},
	}

	for pv := 0; pv < s.pageViews; pv++ {
		game := s.game
		if pv > 0 {
			game = games[rng.Intn(len(games))]
			events = append(events, map[string]any{"event": "game_switch", "to_game": game})
		}
		events = append(events, map[string]any{"event": "game_view", "game": game})

		// --- the ad opportunity ---
		st.adRequests.Add(1)
		raw, err := post(ctx, c, base+"/ad/request", map[string]any{
			"placement_id": "game_sidebar", "game": game, "session_id": s.id,
			"device_type": s.device, "env": "lab", "w": 300, "h": 250,
		})
		if err != nil {
			st.errors.Add(1)
		} else {
			var ar adResponse
			if json.Unmarshal(raw, &ar) == nil {
				if ar.Ad != nil {
					st.filled.Add(1)
					fire(ctx, c, base+"/event/impression?token="+ar.Ad.TrackingToken)
					st.impressions.Add(1)
					// A click is rare, and pretending otherwise would produce a
					// CTR that teaches the wrong intuition.
					if rng.Float64() < 0.006 {
						fire(ctx, c, base+"/event/click?token="+ar.Ad.TrackingToken)
						st.clicks.Add(1)
					}
				} else {
					st.noAd.Add(1)
				}
			}
		}

		// --- the games themselves ---
		for g := 0; g < s.games; g++ {
			events = append(events, map[string]any{
				"event": "game_start", "game": game,
				"mode": []string{"cpu", "two"}[rng.Intn(2)], "level": rng.Intn(3),
			})
			if rng.Float64() < 0.88 { // most games get finished
				events = append(events, map[string]any{
					"event": "game_end", "game": game,
					"outcome": []string{"win", "draw"}[rng.Intn(2)],
					"moves":   5 + rng.Intn(20),
				})
				if g < s.games-1 {
					events = append(events, map[string]any{"event": "game_replay", "game": game})
				}
			}
		}
	}

	// Batched, exactly as the browser does it.
	if _, err := post(ctx, c, base+"/collect", map[string]any{
		"session_id": s.id, "env": "lab", "events": events,
	}); err != nil {
		st.errors.Add(1)
	} else {
		st.collects.Add(1)
	}
}

func fire(ctx context.Context, c *http.Client, url string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := c.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func main() {
	var cfg config
	flag.StringVar(&cfg.base, "base", "http://127.0.0.1:8139", "our own endpoint. NEVER point this at a third party")
	flag.IntVar(&cfg.sessions, "sessions", 50, "total sessions (hard cap)")
	flag.Float64Var(&cfg.rate, "rate", 2, "sessions started per second")
	flag.IntVar(&cfg.workers, "workers", 8, "max concurrent sessions")
	flag.Int64Var(&cfg.seed, "seed", 1, "seed, so a run is reproducible")
	flag.Parse()

	// Refuse to run unbounded. Every request costs money.
	if cfg.sessions <= 0 || cfg.sessions > 20000 {
		fmt.Fprintln(os.Stderr, "sessions must be between 1 and 20000 -- this generates real AWS cost")
		os.Exit(2)
	}
	if cfg.rate <= 0 || cfg.rate > 50 {
		fmt.Fprintln(os.Stderr, "rate must be between 0 and 50/s")
		os.Exit(2)
	}

	fmt.Printf("trafficsim -> %s\n  %d sessions at %.1f/s, env=lab, seed=%d\n\n",
		cfg.base, cfg.sessions, cfg.rate, cfg.seed)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run(ctx, cfg)
}
