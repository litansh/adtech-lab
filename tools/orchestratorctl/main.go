// orchestratorctl -- the Orchestrator agent. What should I do now, and why?
//
//	orchestratorctl next --events '/tmp/events/*.ndjson*' --telegram
//
// The other seven agents each answer a question about the system. This one
// answers a question about the person: given where the plan actually is, what
// the fleet is waiting on, and what the funnel shows, what is the single next
// thing worth doing.
//
// Three rules it does not break:
//
//   - ONE next action. A plan that offers five choices gets none of them done.
//   - The earliest broken funnel stage, and only that one. Work on stage four
//     while stage two is broken cannot show up in the numbers.
//   - It never mutates anything. It is Stage 1 (observe) on the agentkit
//     ladder, and everything it wants changed goes to a human -- on Telegram,
//     with a link, not buried in a pull request body.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// heartbeatDays: send the digest even when nothing has changed, this often.
//
// It was seven, and seven is the alerter's number, borrowed by mistake. The
// Reliability agent should stay silent when nothing is wrong -- it interrupts a
// person, and an alert that fires on a quiet day gets muted.
//
// This is not an alerter. It is a MORNING BRIEFING that answers one question:
// what is the single next thing worth doing. A briefing that arrives only when
// something changed is a briefing nobody can rely on, and its absence is
// unreadable -- the reader cannot tell "nothing changed" from "the agent died",
// which is the exact confusion this tool exists to remove and which this
// project shipped six times in one day.
//
// So: once a day, every day. On a day with nothing to report it says so in a
// line, and that line is the evidence the fleet is alive. Change detection
// still suppresses a duplicate from a manual re-run within the same day, which
// is the only kind of repetition that was ever noise.
//
// Measured in CALENDAR DAYS, not elapsed hours. GitHub's scheduler drifts by
// tens of minutes, so "more than 24 hours since the last send" silently skips a
// morning whenever yesterday's run was delayed and today's was not -- and the
// skipped morning is indistinguishable from a dead agent, which is the whole
// thing this is trying to avoid. "Has one gone out today?" has no such edge.

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "next" && os.Args[1] != "status") {
		fmt.Fprintln(os.Stderr, "orchestratorctl next|status [--state FILE] [--events GLOB] [--telegram] [--force] [--offline]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	// Defaults are anchored to the DISCOVERED repository root.
	//
	// Agents run as `go -C tools/orchestratorctl run .`, so a bare
	// "ROADMAP.md" writes one inside the tool directory -- which is exactly
	// what it did, and the fourth time in a single day that something here
	// assumed the working directory was the repository root. The others were
	// the Telegram path, every Reliability alert, and supplychain --local
	// declaring our own supply chain broken.
	root := repoRoot()
	statePath := fs.String("state", filepath.Join(root, "state/plan.json"), "declared state")
	lastPath := fs.String("last", filepath.Join(root, "state/orchestrator-last.json"), "what was last sent, to avoid repeating it")
	progressPath := fs.String("progress", filepath.Join(root, "state/progress.json"), "what has been finished, and when")
	roadmapPath := fs.String("roadmap", filepath.Join(root, "ROADMAP.md"), "regenerated on every run")
	itchPath := fs.String("itch-history", filepath.Join(root, "state/itch-history.json"), "daily itch totals")
	healthPath := fs.String("health-memory", filepath.Join(root, "state/fleet-health.json"), "what has already been done about a sick agent")
	act := fs.Bool("act", false, "retry and escalate sick agents, rather than only reporting them")
	events := fs.String("events", "", "glob of event files (.ndjson or .ndjson.gz)")
	telegram := fs.Bool("telegram", false, "send the digest to Telegram")
	force := fs.Bool("force", false, "send even if nothing has changed")
	offline := fs.Bool("offline", false, "skip every network check")
	_ = fs.Parse(os.Args[2:])

	st, err := LoadState(*statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "orchestratorctl: %v\n", err)
		fmt.Fprintln(os.Stderr, "Create it from state/plan.example.json — it is four fields.")
		os.Exit(1)
	}

	w := Observe(st, *events, *offline)

	// Loaded before the diagnosis: the funnel needs to know when the first game
	// reached itch, or it judges a day-old listing by the same standard as a
	// month-old one.
	prog := LoadProgress(*progressPath)
	if d, ok := prog.Done["publish-sudoku"]; ok {
		if t, err := time.Parse("2006-01-02", d); err == nil {
			w.LiveSince = t
		}
	}

	// itch, read directly when a key is configured. A measured number always
	// beats a typed one: a figure someone has to copy out of a dashboard is a
	// figure that stops being copied, and `reach` was blind for exactly that
	// reason.
	if key := os.Getenv("ITCH_API_KEY"); key != "" && !*offline {
		hist := LoadItchHistory(*itchPath)
		games, err := FetchItch(key)
		if err != nil {
			w.Notes = append(w.Notes, "itch api: "+err.Error())
		} else {
			hist = RecordReading(hist, games, today(w.Now))
			if err := SaveItchHistory(*itchPath, hist); err != nil {
				w.Notes = append(w.Notes, "itch history: "+err.Error())
			}
			if v := Views7d(hist, w.Now, w.LiveSince); v >= 0 {
				w.ItchViews7d = v
			}
			live := 0
			for _, g := range games {
				if g.Published {
					live++
				}
			}
			if live > 0 {
				w.LiveGames = live // the API knows drafts from published
			}
		}
	}

	as := Assess(w)
	stages, verdict, action := Diagnose(w)

	full := render(w, as, stages, verdict, action, os.Args[1] == "status")
	fmt.Print(full)

	// Finishing something is the one event here that earns a message of its
	// own, so it is detected before the digest and sent before it.
	won := Achievements(prog, as)
	prog = Record(prog, as, today(w.Now))

	if os.Args[1] == "next" {
		if err := os.WriteFile(*roadmapPath, []byte(RenderRoadmap(as, prog, w)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "orchestratorctl: could not write %s: %v\n", *roadmapPath, err)
		}
		if err := SaveProgress(*progressPath, prog); err != nil {
			fmt.Fprintf(os.Stderr, "orchestratorctl: could not record progress: %v\n", err)
		}
	}

	for _, a := range won {
		fmt.Printf("\nDONE    %s\n", a.Step.Title)
	}

	// Detection alone was the previous state: a sick agent got a line in the
	// digest, and if that line went unread nothing happened, forever.
	if *act && len(w.Health) > 0 && os.Args[1] == "next" {
		mem := LoadHealthMemory(*healthPath)
		actions, mem := Decide(w.Health, mem, today(w.Now))
		for i := range actions {
			a := actions[i]
			switch a.Kind {
			case "retry":
				if err := Retry(a.Workflow); err != nil {
					fmt.Fprintf(os.Stderr, "orchestratorctl: could not retry %s: %v\n", a.Agent, err)
				} else {
					fmt.Printf("\nRETRY   %s — %s\n", a.Agent, a.Reason)
				}
			case "escalate":
				url, err := Escalate(a)
				if err != nil {
					fmt.Fprintf(os.Stderr, "orchestratorctl: could not raise %s: %v\n", a.Agent, err)
					continue
				}
				if t := mem.Seen[a.Agent]; t != nil {
					t.IssueURL = url
				}
				fmt.Printf("\nRAISED  %s — %s\n        %s\n", a.Agent, a.Reason, url)
				_ = notify("An agent needs you: "+a.Agent,
					fmt.Sprintf("%s is %s, and a retry did not fix it.\n\n%s", a.Agent, a.Reason, url))
			case "recovered":
				fmt.Printf("\nRECOVER %s — %s\n", a.Agent, a.Reason)
				_ = notify("Recovered: "+a.Agent, a.Agent+" is running again ("+a.Reason+").")
			}
		}
		if err := SaveHealthMemory(*healthPath, mem); err != nil {
			fmt.Fprintf(os.Stderr, "orchestratorctl: could not record what was done: %v\n", err)
		}
	}

	if !*telegram {
		return
	}

	// Sent whatever the digest does. An achievement is never a repeat, so the
	// once-a-day rule below has no business suppressing it.
	if len(won) > 0 {
		if err := notify("Step complete", CelebrationText(won, as, prog)); err != nil {
			fmt.Fprintf(os.Stderr, "orchestratorctl: could not send the achievement: %v\n", err)
		}
	}
	digest := renderDigest(w, as, verdict, action)
	// Printed as well as sent. The message that reaches a phone was previously
	// invisible everywhere else, so the only way to check its wording was to
	// own the phone.
	fmt.Printf("\n--- the message ---\n%s-------------------\n", digest)
	if !*force && !shouldSend(*lastPath, digest, w.Now) {
		fmt.Println("\n(telegram: unchanged since the last send, and the heartbeat is not due)")
		return
	}
	if err := notify("Orchestrator — "+headline(as), digest); err != nil {
		// Loudly, and non-zero.
		//
		// The first live run of this workflow reported success while sending
		// nothing: notify swallowed the error, the digest never arrived, and
		// the run was green. A daily agent whose entire purpose is that a
		// message reaches a phone must go red when the message does not --
		// otherwise a silent failure and a quiet day look identical, which is
		// the failure mode this tool was written to stop making.
		fmt.Fprintf(os.Stderr, "\norchestratorctl: the digest was NOT delivered: %v\n", err)
		os.Exit(1)
	}
	recordSent(*lastPath, digest, w.Now)
}

func headline(as []Assessment) string {
	if n, ok := Next(as); ok {
		return n.Step.Title
	}
	for _, a := range as {
		if a.Status == StatusBlocked {
			return "nothing to do — waiting"
		}
	}
	return "the plan is done"
}

// render is the long form, for a terminal and for the CI log.
func render(w World, as []Assessment, stages []Stage, verdict, action string, full bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "orchestrator — %s\n\n", w.Now.Format("2006-01-02 15:04 UTC"))

	if n, ok := Next(as); ok {
		fmt.Fprintf(&b, "DO NOW  %s  (%d min)\n", n.Step.Title, n.Step.Minutes)
		if n.Note != "" {
			fmt.Fprintf(&b, "        so far: %s\n", n.Note)
		}
		fmt.Fprintf(&b, "        open: %s\n", n.Step.Doc)
		if n.Step.Why != "" {
			fmt.Fprintf(&b, "        why:  %s\n", n.Step.Why)
		}
		var then []string
		for _, a := range as {
			if a.Status == StatusReady && a.Step.ID != n.Step.ID && len(then) < 2 {
				then = append(then, a.Step.Title)
			}
		}
		if len(then) > 0 {
			fmt.Fprintf(&b, "\nTHEN    %s\n", strings.Join(then, "\n        "))
		}
	} else {
		fmt.Fprintf(&b, "DO NOW  nothing — every available step is done or blocked\n")
	}

	// Conflicts first among the rest: a step marked done that the world says is
	// not done is the most expensive thing on this page.
	for _, a := range as {
		if a.Status == StatusConflict {
			fmt.Fprintf(&b, "\nCHECK   %s\n        %s\n", a.Step.Title, a.Note)
		}
	}

	if len(w.OpenPRs)+len(w.OpenIssues) > 0 {
		fmt.Fprintf(&b, "\nFLEET   %d waiting on you\n", len(w.OpenPRs)+len(w.OpenIssues))
		for _, it := range append(append([]Item{}, w.OpenPRs...), w.OpenIssues...) {
			fmt.Fprintf(&b, "        #%d %s\n", it.Number, it.Title)
		}
	}

	// Health BEFORE capability. Whether an agent could run is a much less
	// interesting question than whether it did, and reporting the first while
	// the second is broken is exactly what this report did on the day three
	// agents turned out to be dead.
	fmt.Fprintf(&b, "\nHEALTH  %s\n", healthLine(w.Health))
	for _, h := range w.Health {
		mark := "  "
		if h.Loud() {
			mark = "XX"
		}
		fmt.Fprintf(&b, "        %s %-13s %-13s %s\n", mark, h.Agent.Name, h.State, h.Detail)
	}

	fmt.Fprintf(&b, "\nFLEET   %s\n", fleetLine(w))
	if runnable, waiting := FleetSchedule(w); len(waiting) > 0 {
		for _, a := range runnable {
			fmt.Fprintf(&b, "        run   %-12s %s\n", a.Name, a.Produces)
		}
		for _, a := range waiting {
			fmt.Fprintf(&b, "        wait  %-12s needs traffic — %s\n", a.Name, a.Asks)
		}
	}

	fmt.Fprintf(&b, "\nFUNNEL  %s\n", verdict)
	for _, s := range stages {
		mark := "ok "
		switch {
		case !s.Known:
			mark = "?  "
		case s.TooEarly && !s.OK:
			mark = "···"
		case !s.OK:
			mark = "XX "
		}
		fmt.Fprintf(&b, "        %s %-13s %s\n", mark, s.Name, s.Have)
	}
	fmt.Fprintf(&b, "\nADVICE  %s\n", wrap(action, 8))

	if full {
		fmt.Fprintf(&b, "\nPLAN\n")
		for _, a := range as {
			line := fmt.Sprintf("        %-9s %-10s %s", a.Status, a.Step.Phase, a.Step.Title)
			if a.Note != "" {
				line += "  — " + a.Note
			}
			fmt.Fprintln(&b, line)
		}
	}

	if len(w.Notes) > 0 {
		fmt.Fprintf(&b, "\nCOULD NOT CHECK\n")
		for _, n := range w.Notes {
			fmt.Fprintf(&b, "        %s\n", n)
		}
	}
	return b.String()
}

// renderDigest is the phone version, and a phone is not a terminal.
//
// It used to be the terminal report with the columns removed: labels like
// "FUNNEL:" and "AGENTS:", a file path with no way to open it, an issue number
// with no link. Readable if you already knew what it meant, which is the wrong
// audience for a briefing.
//
// It now answers three questions in order -- what should I do, what is waiting
// on me, is anything wrong -- and every reference is a URL you can tap.
func renderDigest(w World, as []Assessment, verdict, action string) string {
	var b strings.Builder

	if n, ok := Next(as); ok {
		fmt.Fprintf(&b, "▶ DO THIS · %d min\n%s\n", n.Step.Minutes, n.Step.Title)
		if n.Note != "" {
			fmt.Fprintf(&b, "so far: %s\n", n.Note)
		}
		if doc := docLink(n.Step.Doc); doc != "" {
			fmt.Fprintf(&b, "%s\n", doc)
		}
		if n.Step.Why != "" {
			fmt.Fprintf(&b, "\nWhy: %s.\n", n.Step.Why)
		}
	} else {
		// The commonest state, and it deserves a real sentence rather than a
		// blank. "Nothing to do" with no reason reads as a broken agent.
		fmt.Fprintf(&b, "✓ Nothing to do today.\n")
		// Matched on the STATUS, not on a word in the prose. The previous
		// version searched the note for "unblocks", and capitalising that word
		// one commit later silently deleted this whole line from the message --
		// leaving a briefing that said "nothing to do" and nothing else, which
		// is the least useful thing it could have said.
		//
		// A message assembled by grepping its own English is a message one
		// wording change away from being wrong.
		for _, a := range as {
			if a.Status == StatusBlocked && a.Note != "" {
				fmt.Fprintf(&b, "\nNext up: %s\n%s\n", a.Step.Title, a.Note)
				break
			}
		}
	}

	for _, a := range as {
		if a.Status == StatusConflict {
			fmt.Fprintf(&b, "\n⚠ CHECK: %s\n%s\n", a.Step.Title, a.Note)
		}
	}

	// Links, not numbers. An issue number on a phone is a thing to go and look
	// up later, which means never.
	if items := append(append([]Item{}, w.OpenPRs...), w.OpenIssues...); len(items) > 0 {
		fmt.Fprintf(&b, "\n%s waiting on you\n", plural(len(items), "item"))
		for _, it := range items {
			fmt.Fprintf(&b, "· %s\n  %s\n", it.Title, it.URL)
		}
	}

	// Only when something is wrong. On a good day the fleet needs no paragraph.
	if bad := Unhealthy(w.Health); len(bad) > 0 {
		fmt.Fprintf(&b, "\n⚠ %s not running\n", plural(len(bad), "agent"))
		for _, h := range bad {
			fmt.Fprintf(&b, "· %s — %s\n", h.Agent.Name, h.Detail)
		}
	}

	fmt.Fprintf(&b, "\n%s\n%s.\n", verdict, strings.ToUpper(action[:1])+action[1:])
	return b.String()
}

// docLink turns "docs/GO-LIVE.md step 1.2 — ..." into something tappable.
// A file path in a message on a phone is a path to a file you are not near.
func docLink(doc string) string {
	if doc == "" {
		return ""
	}
	path := strings.FieldsFunc(doc, func(r rune) bool { return r == ' ' || r == ',' })[0]
	if !strings.HasSuffix(path, ".md") && !strings.Contains(path, "/") {
		return doc
	}
	rest := strings.TrimSpace(strings.TrimPrefix(doc, path))
	link := "https://github.com/litansh/adtech-lab/blob/main/" + path
	if rest != "" {
		return link + "\n" + strings.TrimPrefix(rest, "— ")
	}
	return link
}

func wrap(s string, indent int) string {
	pad := strings.Repeat(" ", indent)
	words, line, out := strings.Fields(s), "", []string{}
	for _, wd := range words {
		if len(line)+len(wd)+1 > 72 {
			out, line = append(out, line), ""
		}
		if line == "" {
			line = wd
		} else {
			line += " " + wd
		}
	}
	return strings.Join(append(out, line), "\n"+pad)
}

// --- not repeating yourself ------------------------------------------------

type sentRecord struct {
	Hash   string `json:"hash"`
	SentAt string `json:"sent_at"`
}

func shouldSend(path, digest string, now time.Time) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	var r sentRecord
	if json.Unmarshal(b, &r) != nil {
		return true
	}
	if r.Hash != hash(digest) {
		return true
	}
	t, err := time.Parse(time.RFC3339, r.SentAt)
	if err != nil {
		return true // an unreadable record is not evidence that one was sent
	}
	return !sameDay(t.UTC(), now.UTC())
}

// sameDay in UTC, which is also the clock the schedule runs on.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func recordSent(path, digest string, now time.Time) {
	b, _ := json.MarshalIndent(sentRecord{hash(digest), now.Format(time.RFC3339)}, "", "  ")
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "orchestratorctl: could not record the send (%v)\n", err)
	}
}

func hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// repoRoot walks up looking for a marker, so every default path lands in the
// repository rather than wherever the process happened to start.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "PLAN.md")); err == nil {
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

// notifyScript finds telegram.py by walking up from the working directory.
//
// Agents are run as `go -C tools/orchestratorctl run .`, so the working
// directory is the tool, not the repository root. Resolved by LOOKING rather
// than by assuming a depth: a hard-coded "../../" is right until someone moves
// a tool one level deeper, and then it is wrong in the same silent way.
func notifyScript() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, "tools", "notify", "telegram.py")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not find tools/notify/telegram.py above the working directory")
}

func notify(title, body string) error {
	script, err := notifyScript()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "orch-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	// --plain: a briefing is prose with links, not a tool's tabular output.
	cmd := exec.Command("python3", script, "--title", title, "--body-file", f.Name(), "--plain")
	out, err := cmd.CombinedOutput()
	os.Stdout.Write(out)
	if err != nil {
		return fmt.Errorf("%s: %w", script, err)
	}
	// telegram.py exits 0 when it is not configured, on purpose -- the agents
	// must run whether or not anyone is listening. But when this workflow ran
	// it WITH credentials and nothing arrived, that is a failure, so the one
	// case it reports in words has to be read.
	if strings.Contains(string(out), "send failed") {
		return fmt.Errorf("telegram rejected the message")
	}
	return nil
}
