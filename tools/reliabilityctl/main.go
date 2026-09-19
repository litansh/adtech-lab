// reliabilityctl -- the Reliability agent. Can we afford to stay up?
//
//	reliabilityctl check --profile NAME
//
// Reads CloudWatch alarms, Lambda metrics and month-to-date spend, and reports
// what is worth interrupting someone for versus what is worth reading tomorrow.
//
// It is the only agent whose findings are meant to arrive on a phone
// unprompted, which is exactly why it must not report everything.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "check" {
		fmt.Fprintln(os.Stderr, "reliabilityctl check [--profile NAME] [--region NAME] [--ceiling USD]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	profile := fs.String("profile", "", "AWS CLI profile")
	region := fs.String("region", "us-east-1", "AWS region")
	ceiling := fs.Float64("ceiling", 100, "monthly ceiling in USD")
	credits := fs.Float64("credits", 119.94, "credits remaining")
	expiry := fs.String("credit-expiry", "2027-02-08", "when credits expire")
	_ = fs.Parse(os.Args[2:])

	now := time.Now().UTC()
	s := Signals{
		CeilingUSD:     *ceiling,
		CreditsLeftUSD: *credits,
		DaysIntoMonth:  now.Day(),
		DaysInMonth:    daysInMonth(now),
		Alarms:         map[string]string{},
	}
	if t, err := time.Parse("2006-01-02", *expiry); err == nil {
		s.DaysToExpiry = int(t.Sub(now).Hours() / 24)
	}

	// Every read is best-effort. A reliability agent that fails because one
	// metric was unavailable is a reliability agent that reports nothing on
	// exactly the day something is wrong.
	s.MonthToDateUSD = costThisMonth(*profile, *region, now)
	s.Alarms = alarmStates(*profile, *region)
	s.Invocations, s.Errors = lambdaCounts(*profile, *region)

	findings := Assess(s)
	worst := Worst(findings)

	fmt.Printf("reliability: %s\n\n", worst)
	for _, f := range findings {
		fmt.Printf("  %-5s %-9s %s\n", f.Severity, f.Check, f.Detail)
		if f.Action != "" {
			fmt.Printf("        %s\n", f.Action)
		}
	}

	// Only PAGE and NOTE are worth a message. OK is a summary line in a log.
	if worst != SevOK {
		var b strings.Builder
		for _, f := range findings {
			if f.Severity == SevOK {
				continue
			}
			fmt.Fprintf(&b, "%s %s: %s\n", f.Severity, f.Check, f.Detail)
			if f.Action != "" {
				fmt.Fprintf(&b, "  %s\n", f.Action)
			}
		}
		if err := notify("Reliability: "+string(worst), b.String()); err != nil {
			// An undelivered page is a failure of the only thing this agent
			// exists to do, so the workflow must go red for it.
			fmt.Fprintf(os.Stderr, "reliabilityctl: the alert was NOT delivered: %v\n", err)
			os.Exit(1)
		}
	}

	// A PAGE exits non-zero so a workflow shows red.
	if worst == SevPage {
		os.Exit(1)
	}
}

func awsArgs(profile, region string, rest ...string) []string {
	a := []string{}
	if profile != "" {
		a = append(a, "--profile", profile)
	}
	a = append(a, "--region", region, "--output", "json")
	return append(a, rest...)
}

func run(args ...string) (string, bool) {
	out, err := exec.Command("aws", args...).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

func costThisMonth(profile, region string, now time.Time) float64 {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	out, ok := run(awsArgs(profile, region, "ce", "get-cost-and-usage",
		"--time-period", fmt.Sprintf("Start=%s,End=%s",
			start.Format("2006-01-02"), now.Format("2006-01-02")),
		"--granularity", "MONTHLY", "--metrics", "UnblendedCost")...)
	if !ok {
		return 0
	}
	var r struct {
		ResultsByTime []struct {
			Total struct {
				UnblendedCost struct{ Amount string } `json:"UnblendedCost"`
			} `json:"Total"`
		} `json:"ResultsByTime"`
	}
	if json.Unmarshal([]byte(out), &r) != nil || len(r.ResultsByTime) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(r.ResultsByTime[0].Total.UnblendedCost.Amount, 64)
	return v
}

func alarmStates(profile, region string) map[string]string {
	states := map[string]string{}
	out, ok := run(awsArgs(profile, region, "cloudwatch", "describe-alarms")...)
	if !ok {
		return states
	}
	var r struct {
		MetricAlarms []struct {
			AlarmName  string `json:"AlarmName"`
			StateValue string `json:"StateValue"`
		} `json:"MetricAlarms"`
	}
	if json.Unmarshal([]byte(out), &r) != nil {
		return states
	}
	for _, a := range r.MetricAlarms {
		states[a.AlarmName] = a.StateValue
	}
	return states
}

func lambdaCounts(profile, region string) (invocations, errors int) {
	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)
	get := func(metric string) int {
		out, ok := run(awsArgs(profile, region, "cloudwatch", "get-metric-statistics",
			"--namespace", "AWS/Lambda", "--metric-name", metric,
			"--dimensions", "Name=FunctionName,Value=adtech-lab-ad-server",
			"--start-time", start.Format(time.RFC3339),
			"--end-time", end.Format(time.RFC3339),
			"--period", "86400", "--statistics", "Sum")...)
		if !ok {
			return 0
		}
		var r struct {
			Datapoints []struct{ Sum float64 } `json:"Datapoints"`
		}
		if json.Unmarshal([]byte(out), &r) != nil {
			return 0
		}
		total := 0.0
		for _, d := range r.Datapoints {
			total += d.Sum
		}
		return int(total)
	}
	return get("Invocations"), get("Errors")
}

// notifyScript finds telegram.py by walking UP from the working directory.
//
// Agents run as `go -C tools/<name> run .`, so the working directory is the
// tool, not the repository root. Resolved by looking rather than by assuming a
// depth: a hard-coded "../../" is right until someone moves a tool one level
// deeper, and then it is wrong in the same silent way this bug already was.
func notifyScript() (string, error) {
	if p := os.Getenv("ADLAB_NOTIFY"); p != "" {
		return p, nil
	}
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

// notify posts to Telegram if it is configured.
//
// It used to be best-effort in the strongest sense: it built a command with a
// path that never resolved and discarded the result, so this agent -- the one
// whose entire job is paging a human when spend or alarms go wrong -- had never
// delivered a single message. An alerting agent that fails silently is worse
// than no alerting agent, because it also produces the belief that someone is
// watching. So a failed send is now reported and returned.
func notify(title, body string) error {
	if os.Getenv("TELEGRAM_BOT_TOKEN") == "" {
		return nil // not configured is not an error; the check still ran
	}
	script, err := notifyScript()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "rel-*.txt")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(body); err != nil {
		f.Close()
		return err
	}
	f.Close()

	out, err := exec.Command("python3", script, "--title", title, "--body-file", f.Name()).CombinedOutput()
	os.Stdout.Write(out)
	if err != nil {
		return fmt.Errorf("%s: %w", script, err)
	}
	if strings.Contains(string(out), "send failed") {
		return fmt.Errorf("telegram rejected the message")
	}
	return nil
}

func daysInMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
