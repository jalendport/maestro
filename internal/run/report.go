package run

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ui"
)

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
	w.WriteString(ModeLine(s) + "\n")
	if line := s.LandingCheck.Line(); line != "" {
		w.WriteString(line + "\n")
	}
	if s.PlanCheckNote != "" {
		w.WriteString(s.PlanCheckNote + "\n")
	}
	for _, branch := range s.KeptBranches {
		fmt.Fprintf(&w, "kept branch %s\n", branch)
	}
	for _, t := range sortedTasks(s) {
		fmt.Fprintf(&w, "  %-2d %-32s %-8s %-18s %d  %s\n", t.Number, cutTitle(t.Title, 32), stateWord(t), laneWord(t), t.Attempt, reportPath(s, t))
		if order := OrderLine(t); order != "" {
			fmt.Fprintf(&w, "    %s\n", order)
		}
		if (t.State == TaskBlocked || t.State == TaskFailed) && t.Workdir != "" {
			fmt.Fprintf(&w, "    Worktree: %s\n", t.Workdir)
		}
		if t.Attempt > 1 {
			folder := t.Folder
			if folder == "" {
				folder = filepath.Join(s.Folder, t.ID)
			}
			judged := t.JudgedFiles
			if judged == nil {
				if rec, err := dispatch.LoadRecord(folder); err == nil {
					judged = append([]string{}, rec.Verification.Changed...)
				}
			}
			if judged != nil {
				files := strings.Join(judged, ", ")
				if files == "" {
					files = "none"
				}
				fmt.Fprintf(&w, "    judged files: %s\n", files)
			}
		}
		folder := t.Folder
		if folder == "" {
			folder = filepath.Join(s.Folder, t.ID)
		}
		if rec, err := dispatch.LoadRecord(folder); err == nil && len(rec.Verification.TestsOutsideTouch) > 0 {
			if t.Attempt <= 1 {
				fmt.Fprintf(&w, "    changed: %s\n", strings.Join(rec.Verification.Changed, ", "))
			}
			fmt.Fprintf(&w, "    tests outside touch: %s\n", strings.Join(rec.Verification.TestsOutsideTouch, ", "))
		}
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
		fmt.Fprintf(&w, "resume with: %s\n", ResumeCommand(s, ResumeTask(s)))
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
	text := ui.FirstLine(t.Error, 80)
	if len(t.Conflicts) > 0 {
		text = "merge conflict in " + strings.Join(t.Conflicts, ", ") + "; Worktree: " + t.Workdir
	}
	if rec, err := dispatch.LoadRecord(t.Folder); err == nil {
		if len(t.Conflicts) == 0 && rec.BlockedReason() != "" {
			text = rec.BlockedReason()
		}
		if len(rec.Verification.TestsOutsideTouch) > 0 {
			if text != "" {
				text += "; "
			}
			text += "tests outside touch: " + strings.Join(rec.Verification.TestsOutsideTouch, ", ")
		}
	}
	return text
}

// Status renders the live view of maestro status.
func Status(s *State) string {
	var w strings.Builder
	done, _, _, _, _, _ := s.Counts()
	awake := ""
	if s.Awake {
		awake = "  caffeinate on"
	}
	state := s.State
	stale := s.State == StateLive && !SupervisorAlive(s)
	if stale {
		state = "stale (process gone)"
	}
	fmt.Fprintf(&w, "run %s  %s  started %s  %s  %d of %d done%s\n", s.Run, s.Title, s.StartedAt.Format("15:04"), state, done, len(s.Tasks), awake)
	w.WriteString(ModeLine(s) + "\n")
	if line := s.LandingCheck.Line(); line != "" {
		w.WriteString(line + "\n")
	}
	if s.PlanCheckNote != "" {
		w.WriteString(s.PlanCheckNote + "\n")
	}
	if s.State == StateHeld {
		for _, a := range s.Approval {
			fmt.Fprintf(&w, "approval needed: %s\n", a)
		}
		fmt.Fprintf(&w, "run %s held.  Fold any findings into the Plan file, then: maestro approve %s\n", s.Run, s.Run)
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
		taskState := stateWord(t)
		if t.State == TaskRunning {
			rec, err := dispatch.LoadRecord(filepath.Join(s.Folder, t.ID))
			if stale || (err == nil && rec.State == "running" && !dispatch.ProcessAlive(rec)) {
				taskState = "stale (process gone)"
			} else if err == nil && dispatch.ChecksRunning(rec) {
				taskState = "running (checks running)"
			}
		}
		fmt.Fprintf(&w, "  %-2d %-32s %-9s %-26s %s\n", t.Number, cutTitle(t.Title, 32), taskState, lane, tail)
		if order := OrderLine(t); order != "" {
			fmt.Fprintf(&w, "    %s\n", order)
		}
		if rec, err := dispatch.LoadRecord(filepath.Join(s.Folder, t.ID)); err == nil {
			if len(rec.Verification.TestsOutsideTouch) > 0 {
				fmt.Fprintf(&w, "    changed: %s\n    tests outside touch: %s\n", strings.Join(rec.Verification.Changed, ", "), strings.Join(rec.Verification.TestsOutsideTouch, ", "))
			}
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
