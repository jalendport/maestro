package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/ui"
)

// ProcessAlive checks the Run supervisor, the standalone supervisor, or the Lane.
func ProcessAlive(rec *Record) bool {
	folder := filepath.Dir(rec.Path)
	if b, err := os.ReadFile(filepath.Join(folder, "run.json")); err == nil {
		var state struct {
			Pid int `json:"pid"`
		}
		if json.Unmarshal(b, &state) != nil {
			return false
		}
		if pid := supervisorPID(folder); pid > 0 {
			return proc.Alive(pid)
		}
		return proc.Alive(state.Pid)
	}
	if pid := supervisorPID(rec.Path); pid > 0 {
		return proc.Alive(pid)
	}
	return proc.Alive(rec.Pid)
}

func supervisorPID(folder string) int {
	if b, err := os.ReadFile(filepath.Join(folder, "supervisor.pid")); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// ChecksRunning reports whether the live Dispatch has reached its checks.
func ChecksRunning(rec *Record) bool {
	if rec.State != "running" {
		return false
	}
	for _, event := range rec.Events {
		if event.Kind == "check" {
			return true
		}
	}
	return false
}

// List lists standalone Dispatches that are running or ended in the last hour.
// Tasks in a Run are shown by the Run, and Council Seats live elsewhere.
func List(root string) []*Record {
	out := []*Record{}
	runs, _ := RunFolders(root)
	for _, r := range runs {
		if !r.IsDir() {
			continue
		}
		folder := filepath.Join(config.RunsDir(root), r.Name())
		if _, err := os.Stat(filepath.Join(folder, "run.json")); !os.IsNotExist(err) {
			continue
		}
		tasks, _ := os.ReadDir(folder)
		for _, t := range tasks {
			if !t.IsDir() {
				continue
			}
			rec, err := LoadRecord(filepath.Join(folder, t.Name()))
			if err != nil {
				continue
			}
			if rec.State == "running" || (rec.State == "ended" && !rec.EndedAt.IsZero() && time.Since(rec.EndedAt) <= time.Hour) {
				out = append(out, rec)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].State == "running") != (out[j].State == "running") {
			return out[i].State == "running"
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

// Status renders one Dispatch line, including its Lane and attempt.
func Status(rec *Record) string {
	lane := "—"
	if rec.Route != nil {
		lane = rec.Route.FamilyID + " " + rec.Route.Effort
	}
	state := rec.State
	elapsed := rec.DurationS
	if state == "running" {
		if !ProcessAlive(rec) {
			state = "stale (process gone)"
		} else if ChecksRunning(rec) {
			state = "running (checks running)"
		}
		if !rec.StartedAt.IsZero() {
			elapsed = time.Since(rec.StartedAt).Seconds()
		}
	} else if rec.Outcome.Outcome != "" {
		state = rec.Outcome.Outcome
	}
	if rec.Outcome.Outcome == OutcomeUnverified && rec.Outcome.ErrorText != "" {
		state += " (" + rec.Outcome.ErrorText + ")"
	}
	duration := "—"
	if !rec.StartedAt.IsZero() {
		duration = ui.Duration(time.Duration(elapsed * float64(time.Second)))
	}
	text := fmt.Sprintf("  task %s  %s  %s  attempt %d  %s  %s\n", rec.Task, rec.TaskInfo.Title, lane, rec.Attempt, duration, state)
	if rec.Workdir != "" && rec.Workdir != rec.Project {
		text += fmt.Sprintf("workdir %s\n", rec.Workdir)
	}
	var gate Gate
	if json.Unmarshal(rec.Verification.Gate, &gate) == nil {
		if gate.Scored {
			text += fmt.Sprintf("gate     satisfies %.2f  verdict %s  risk %s\n", gate.Satisfies, gate.Verdict, gate.Risk)
		} else if gate.Cause != "" {
			text += fmt.Sprintf("gate     cause %s (%s)\n", gate.Cause, gate.CauseSource)
		}
	}
	return text + RelayEvents(rec)
}

func RelayEvents(rec *Record) string {
	var text string
	for _, event := range rec.Events {
		if event.Kind == "relay" || event.Kind == "relay-answer" {
			text += fmt.Sprintf("%s  task %s  %s\n", event.Kind, rec.Task, event.Text)
			if event.Kind == "relay" {
				id, _, _ := strings.Cut(event.Text, " ")
				text += fmt.Sprintf("         maestro answer %s allow|deny --relay %s\n", rec.Task, id)
			}
		}
	}
	return text
}
