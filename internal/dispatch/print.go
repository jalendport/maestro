package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/proc"
)

// Print writes the human result of one Dispatch
// and returns the exit code: 0 verified, 1 failed, 2 needs the Orchestrator.
func Print(rec *Record, asJSON bool) int {
	if asJSON {
		b, _ := json.MarshalIndent(rec, "", "  ")
		fmt.Fprintln(os.Stdout, string(b))
		return exitCode(rec)
	}
	status := rec.Outcome.Status
	if status == "" {
		status = rec.Outcome.FailureClass
	}
	if rec.Outcome.Outcome == OutcomeUnverified && rec.Outcome.ErrorText != "" {
		status += " (" + rec.Outcome.ErrorText + ")"
	}
	lane := rec.Route.FamilyID
	if rec.Route.Effort != "" {
		lane += " " + rec.Route.Effort
	}
	fmt.Fprintf(os.Stdout, "task %s  %-32s %-9s %-20s attempt %d  %s\n", rec.Task, cutTitle(rec.TaskInfo.Title, 32), status, lane, rec.Attempt, fmtDur(rec.DurationS))
	if rec.Outcome.FailureClass != "" && rec.Outcome.Status == "" {
		fmt.Fprintf(os.Stdout, "error    %s\n", rec.Outcome.ErrorText)
		if rec.Outcome.Timer != "" {
			fmt.Fprintf(os.Stdout, "timer    %s limit\n", rec.Outcome.Timer)
		}
	}
	report, _ := rec.Report()
	if report != nil && report.Status == brief.StatusBlocked {
		fmt.Fprintln(os.Stdout, "blocked")
		for _, b := range report.Blocked {
			fmt.Fprintf(os.Stdout, "         %s\n", b)
		}
	}
	if len(rec.Verification.Changed) > 0 {
		fmt.Fprintf(os.Stdout, "changed  %s\n", strings.Join(rec.Verification.Changed, "  "))
	} else if rec.TaskInfo.Access == brief.AccessEdit && rec.Outcome.Status != "" {
		fmt.Fprintln(os.Stdout, "changed  (nothing)")
	}
	if len(rec.Verification.TouchViolations) > 0 {
		fmt.Fprintf(os.Stdout, "scope    outside touch: %s\n", strings.Join(rec.Verification.TouchViolations, "  "))
	}
	for i, c := range rec.Verification.Checks {
		label := "checks"
		if i > 0 {
			label = ""
		}
		res := "ok"
		if !c.OK {
			res = "failed"
		}
		fmt.Fprintf(os.Stdout, "%-8s %-40s %s\n", label, c.Command, res)
	}
	if len(rec.Verification.Gate) > 0 {
		var g Gate
		if json.Unmarshal(rec.Verification.Gate, &g) == nil {
			if g.Skipped != "" {
				fmt.Fprintf(os.Stdout, "gate     skipped (%s)\n", g.Skipped)
			} else {
				fmt.Fprintf(os.Stdout, "gate     satisfies %.2f  verdict %s  risk %s\n", g.Satisfies, g.Verdict, g.Risk)
			}
		}
	}
	if rec.Verification.CommitRange != "" {
		fmt.Fprintf(os.Stdout, "commits  %s\n", rec.Verification.CommitRange)
	}
	if report != nil && report.Status != brief.StatusBlocked {
		if len(report.Blocked) > 0 {
			fmt.Fprintf(os.Stdout, "blocked  %s\n", strings.Join(report.Blocked, "; "))
		} else if rec.TaskInfo.Access == brief.AccessEdit {
			fmt.Fprintln(os.Stdout, "blocked  (none)")
		}
	}
	if rec.Outcome.ModelRan != "" && rec.Outcome.ModelRan != rec.Route.Model && !strings.Contains(rec.Outcome.ModelRan, rec.Route.Model) {
		fmt.Fprintf(os.Stdout, "model    ran %s (requested %s)\n", rec.Outcome.ModelRan, rec.Route.Model)
	}
	reportPath := filepath.Join(rec.Path, FileReport)
	if report != nil {
		fmt.Fprintf(os.Stdout, "report   %s\n", reportPath)
		fmt.Fprintln(os.Stdout)
		if len(report.Raw) <= 4096 {
			fmt.Fprintln(os.Stdout, strings.TrimSpace(report.Raw))
		} else {
			lines := strings.Split(report.Raw, "\n")
			if len(lines) > 40 {
				lines = lines[:40]
			}
			fmt.Fprintln(os.Stdout, strings.Join(lines, "\n"))
			fmt.Fprintf(os.Stdout, "(%d KB, see report)\n", len(report.Raw)/1024)
		}
	}
	return exitCode(rec)
}

func exitCode(rec *Record) int {
	switch rec.Outcome.Outcome {
	case OutcomeVerified, OutcomeAnswered, OutcomeUnverified:
		return proc.CodeOK
	case OutcomeBlocked:
		return proc.CodeOrchestrator
	}
	return proc.CodeFailed
}

func cutTitle(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func fmtDur(s float64) string {
	d := time.Duration(s * float64(time.Second)).Round(time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
