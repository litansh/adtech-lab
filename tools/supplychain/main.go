// supplychain -- verify a schain the way a buyer does.
//
//	supplychain --local          verify our own files, from disk
//	supplychain --schain FILE    verify a schain JSON document over the network
//
// Exit code is non-zero when any check FAILs, so it can gate a deploy.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	local := flag.Bool("local", false, "verify our own ads.txt/sellers.json from disk")
	schainPath := flag.String("schain", "", "schain JSON document to verify")
	// Defaults to the discovered repository root, not ".".
	//
	// It was ".", and agents run as `go -C tools/supplychain run .`, so --local
	// looked for apps/publisher/sellers.json inside tools/supplychain and
	// reported our own supply chain as BROKEN. The file was there all along.
	//
	// Third time today a tool assumed the working directory was the repository
	// root. CI now runs each no-traffic agent from its own directory for
	// exactly this reason.
	root := flag.String("root", repoRoot(), "repository root, for --local")
	flag.Parse()

	var chain SChain
	var fetch Fetcher

	if *local {
		// Our own identity, as the ad server sends it.
		chain = SChain{Complete: 1, Ver: "1.0", Nodes: []SChainNode{
			{ASI: "xoxoxo.live", SID: "xoxoxo-1", HP: 1},
		}}
		fetch = diskFetcher(*root)
		fmt.Println("verifying our own supply chain, from disk")
	} else {
		if *schainPath == "" {
			fmt.Fprintln(os.Stderr, "supplychain: --local or --schain FILE")
			os.Exit(2)
		}
		raw, err := os.ReadFile(*schainPath)
		if err != nil {
			die(err)
		}
		if err := json.Unmarshal(raw, &chain); err != nil {
			die(err)
		}
		fetch = httpFetcher()
		fmt.Println("verifying over the network")
	}

	findings := Verify(chain, fetch)
	for _, f := range findings {
		fmt.Printf("  %-4s %-28s %s\n", f.Level, f.Check, f.Detail)
	}
	worst := Worst(findings)
	fmt.Printf("\nresult: %s\n", worst)
	if worst == "FAIL" {
		fmt.Println("A buyer that verifies supply chains would refuse this inventory.")
		os.Exit(1)
	}
}

// repoRoot walks up looking for a marker, so a tool works from anywhere.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "apps", "publisher", "ads.txt")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}

// diskFetcher maps the URLs a buyer would fetch onto our repository, so the
// same verifier checks our files before they are published.
func diskFetcher(root string) Fetcher {
	return func(url string) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, "/sellers.json"):
			return os.ReadFile(filepath.Join(root, "apps/publisher/sellers.json"))
		case strings.HasSuffix(url, "/ads.txt"):
			return os.ReadFile(filepath.Join(root, "apps/publisher/ads.txt"))
		}
		return nil, fmt.Errorf("no local mapping for %s", url)
	}
}

func httpFetcher() Fetcher {
	c := &http.Client{Timeout: 10 * time.Second}
	return func(url string) ([]byte, error) {
		resp, err := c.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("http %d", resp.StatusCode)
		}
		return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "supplychain:", err)
	os.Exit(1)
}
