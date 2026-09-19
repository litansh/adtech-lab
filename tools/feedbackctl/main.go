// feedbackctl -- the Feedback agent. What players say, against what they do.
//
//	feedbackctl review --comments comments.json --metrics metrics.json
//
// Reports only, permanently. Its input is untrusted text written by strangers;
// an agent that reads comments and can also change the product is a
// prompt-injection path with extra steps.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "review" {
		fmt.Fprintln(os.Stderr,
			"feedbackctl review --comments FILE [--metrics FILE]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	commentsPath := fs.String("comments", "", "JSON array of comments")
	metricsPath := fs.String("metrics", "", "JSON object of metric -> value")
	_ = fs.Parse(os.Args[2:])
	if *commentsPath == "" {
		fmt.Fprintln(os.Stderr, "feedbackctl: --comments is required")
		os.Exit(2)
	}

	raw, err := os.ReadFile(*commentsPath)
	if err != nil {
		die(err)
	}
	var comments []Comment
	if err := json.Unmarshal(raw, &comments); err != nil {
		die(err)
	}

	metrics := map[string]float64{}
	if *metricsPath != "" {
		mraw, err := os.ReadFile(*metricsPath)
		if err != nil {
			die(err)
		}
		if err := json.Unmarshal(mraw, &metrics); err != nil {
			die(err)
		}
	}

	themes, quarantined := Cluster(comments)
	findings := Reconcile(themes, metrics)

	fmt.Printf("%d comments, %d themes\n\n", len(comments), len(themes))

	for _, f := range findings {
		fmt.Printf("  %-11s %s\n", "["+f.Confidence+"]", f.Label)
		fmt.Printf("              %d mention(s), %d upvote(s), from %v\n",
			f.Mentions, f.Upvotes, f.Sources)
		if f.Evidence.Available {
			agree := "DISAGREES"
			if f.Evidence.Supports {
				agree = "agrees"
			}
			fmt.Printf("              data %s: %s\n", agree, f.Evidence.Note)
		} else {
			fmt.Printf("              %s\n", f.Evidence.Note)
		}
		fmt.Printf("              -> %s\n\n", f.Action)
	}

	if len(quarantined) > 0 {
		fmt.Printf("QUARANTINED (%d) -- these address an automated reader and were\n",
			len(quarantined))
		fmt.Println("not summarised, because a summary is already an act of obedience:")
		for _, q := range quarantined {
			fmt.Printf("  %-14s %s  [marker: %q]\n",
				q.Comment.Source, truncate(q.Comment.Text, 60), q.Marker)
		}
		fmt.Println("\nRead these yourself. Do not paste them into anything.")
	}

	fmt.Println("\nReports only. This agent never changes the product -- its input is")
	fmt.Println("untrusted text, and a human decides what any of it means.")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "feedbackctl:", err)
	os.Exit(1)
}
