package run

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ui"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// Report renders report.md: a header, one line per Task, then the items
// that need the Orchestrator. Under 40 lines.
func Report(s *State) string {
	var w strings.Builder
	done, blocked, failed, _, waiting, skipped := s.Counts()
	ended := s.EndedAt
	if ended.IsZero() {
		ended = time.Now()
	}
	fmt.Fprintf(&w, "run %s  %s  %s %s  %s  %d tasks: %d done", s.Run, s.Title, s.State, ended.Format("15:04"), ui.Duration(ended.Sub(s.StartedAt)), len(s.Tasks), done)
	if blocked > 0 {
		fmt.Fprintf(&w, ", %d blocked", blocked)
	}
	if failed > 0 {
		fmt.Fprintf(&w, ", %d failed", failed)
	}
	if waiting > 0 {
		fmt.Fprintf(&w, ", %d not started", waiting)
	}
	if skipped > 0 {
		fmt.Fprintf(&w, ", %d skipped", skipped)
	}
	if s.PauseReason != "" {
		fmt.Fprintf(&w, "\npaused: %s", s.PauseReason)
	}
	w.WriteString("\n")
	for _, t := range sortedTasks(s) {
		fmt.Fprintf(&w, "  %-2d %-32s %-8s %-18s %d  %s\n", t.Number, cutTitle(t.Title, 32), stateWord(t), laneWord(t), t.Attempt, reportPath(s, t))
	}
	var needs []string
	for _, t := range sortedTasks(s) {
		switch t.State {
		case TaskBlocked:
			needs = append(needs, fmt.Sprintf("task %d  blocked: %s  → answer, edit the Brief, or skip (maestro resume %s --task %d ...)", t.Number, blockedText(t), s.Run, t.Number))
		case TaskFailed:
			needs = append(needs, fmt.Sprintf("task %d  failed after %d attempt(s): %s  → maestro resume %s --task %d --lane F | --brief <file> | --skip", t.Number, t.Attempt, ui.FirstLine(t.Error, 80), s.Run, t.Number))
		case TaskWaiting:
			if t.Waiting != "" {
				needs = append(needs, fmt.Sprintf("task %d  %s", t.Number, t.Waiting))
			}
		}
	}
	if len(needs) > 0 {
		w.WriteString("needs you\n")
		for _, n := range needs {
			w.WriteString("  " + n + "\n")
		}
	}
	if s.State == StatePaused {
		fmt.Fprintf(&w, "resume with: maestro resume %s --task <n> (--lane F[:E] | --brief <file> | --skip)\n", s.Run)
	}
	councils := []string{}
	for _, slug := range s.Councils {
		if strings.HasPrefix(slug, s.Run+"-review") {
			councils = append(councils, slug)
		}
	}
	if len(councils) == 0 {
		reason := s.CouncilSkip
		if reason == "" {
			reason = "Run did not finish"
		}
		fmt.Fprintf(&w, "end-of-run council: not run (%s)\n", reason)
	} else {
		fmt.Fprintf(&w, "end-of-run council: %s  → maestro wait %s\n", councils[0], councils[0])
	}
	return w.String()
}

func stateWord(t *TaskState) string {
	if t.State == TaskDone && t.Outcome == dispatch.OutcomeUnverified {
		return "unverif."
	}
	if t.State == TaskDone && t.Status != "" {
		return t.Status
	}
	return t.State
}

func laneWord(t *TaskState) string {
	if t.Route == nil {
		return "—"
	}
	if t.Route.Effort != "" {
		return t.Route.FamilyID + " " + t.Route.Effort
	}
	return t.Route.FamilyID
}

func reportPath(s *State, t *TaskState) string {
	if t.State == TaskWaiting || t.State == TaskSkipped || t.State == TaskCancelled {
		return ""
	}
	rel, err := filepath.Rel(s.Project, filepath.Join(t.Folder, dispatch.FileReport))
	if err != nil {
		return filepath.Join(t.Folder, dispatch.FileReport)
	}
	return rel
}

func blockedText(t *TaskState) string {
	if rec, err := dispatch.LoadRecord(t.Folder); err == nil && len(rec.Permission.Blocked) > 0 {
		return strings.Join(rec.Permission.Blocked, "; ")
	}
	return ui.FirstLine(t.Error, 80)
}

// Status renders the live view of maestro status.
func Status(s *State) string {
	var w strings.Builder
	done, _, _, _, _, _ := s.Counts()
	awake := ""
	if s.Awake {
		awake = "  caffeinate on"
	}
	fmt.Fprintf(&w, "run %s  %s  started %s  %s  %d of %d done%s\n", s.Run, s.Title, s.StartedAt.Format("15:04"), s.State, done, len(s.Tasks), awake)
	if s.State == StateHeld {
		for _, a := range s.Approval {
			fmt.Fprintf(&w, "approval needed: %s\n", a)
		}
		fmt.Fprintf(&w, "approve with: maestro approve %s   (edit the Plan file first if needed, then run it again)\n", s.Run)
	}
	if s.PauseReason != "" {
		fmt.Fprintf(&w, "paused: %s\n", s.PauseReason)
	}
	now := time.Now()
	for _, t := range sortedTasks(s) {
		elapsed := "—"
		if !t.StartedAt.IsZero() {
			ended := t.EndedAt
			if t.State == TaskRunning {
				ended = now
			}
			elapsed = ui.Duration(ended.Sub(t.StartedAt))
		}
		var tail string
		switch t.State {
		case TaskRunning:
			tail = fmt.Sprintf("%-4d %s   idle %s", t.Attempt+1, elapsed, ui.Duration(now.Sub(t.LastOutput)))
		case TaskWaiting:
			tail = fmt.Sprintf("—    %s", t.Waiting)
		default:
			tail = fmt.Sprintf("%-4d %s", t.Attempt, elapsed)
		}
		lane := laneWord(t)
		if t.Provisional {
			lane += " (provisional)"
		}
		fmt.Fprintf(&w, "  %-2d %-32s %-9s %-26s %s\n", t.Number, cutTitle(t.Title, 32), stateWord(t), lane, tail)
		if rec, err := dispatch.LoadRecord(filepath.Join(s.Folder, t.ID)); err == nil {
			w.WriteString(dispatch.RelayEvents(rec))
		}
	}
	return w.String()
}

func cutTitle(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
