package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/ui"
)

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

// Status renders one Dispatch line, including its Lane, attempt, and tab.
func Status(rec *Record) string {
	lane := "—"
	if rec.Route != nil {
		lane = rec.Route.FamilyID + " " + rec.Route.Effort
	}
	state := rec.State
	elapsed := rec.DurationS
	if state == "running" {
		if !rec.StartedAt.IsZero() {
			elapsed = time.Since(rec.StartedAt).Seconds()
		}
	} else if rec.Outcome.Outcome != "" {
		state = rec.Outcome.Outcome
	}
	if rec.Outcome.Outcome == OutcomeUnverified && rec.Outcome.ErrorText != "" {
		state += " (" + rec.Outcome.ErrorText + ")"
	}
	tab := "tab no"
	if rec.SoloProcess > 0 {
		tab = "tab yes"
	}
	duration := "—"
	if !rec.StartedAt.IsZero() {
		duration = ui.Duration(time.Duration(elapsed * float64(time.Second)))
	}
	return fmt.Sprintf("  task %s  %s  %s  attempt %d  %s  %s  %s\n", rec.Task, rec.TaskInfo.Title, lane, rec.Attempt, duration, tab, state) + RelayEvents(rec)
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
