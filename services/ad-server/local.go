//go:build !lambda

package main

import (
	"log"
	"net/http"
	"os"
)

func serve(s *Server) {
	addr := os.Getenv("ADLAB_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8138"
	}
	log.Printf("ad-server listening on %s (lab=%v, line_items=%d)", addr, s.lab, len(s.cp().LineItems))
	log.Fatal(http.ListenAndServe(addr, s.routes()))
}
