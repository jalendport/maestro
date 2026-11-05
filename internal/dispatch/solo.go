package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/solo"
	"github.com/jalendport/maestro/internal/ui"
)

// SoloLink is Maestro's open session to Solo, nil outside Solo.
type SoloLink struct {
	Client    *solo.Client
	ProjectID int
	// Orchestrator is the agent process that gets wake-up timers.
	Orchestrator int
}

// ConnectSolo opens the link when this process runs inside Solo. A detached
// supervisor keeps SOLO_* in its environment, so the link survives detach.
func ConnectSolo() *SoloLink {
	if !solo.InSolo() {
		return nil
	}
	c, err := solo.Connect()
	if err != nil {
		ui.Warnf("solo: %v", err)
		return nil
	}
	link := &SoloLink{Client: c, Orchestrator: solo.ProcessID()}
	if id, err := c.WhoAmI(); err == nil && id.ProjectID != 0 {
		link.ProjectID = id.ProjectID
	} else if n := os.Getenv("SOLO_PROJECT_ID"); n != "" {
		fmt.Sscanf(n, "%d", &link.ProjectID)
	}
	if link.ProjectID == 0 {
		c.Close()
		return nil
	}
	return link
}

// Close ends the session.
func (l *SoloLink) Close() {
	if l != nil && l.Client != nil {
		l.Client.Close()
	}
}

// TodoFor adopts a named todo or creates the Task's todo (Tasks are Solo todos) and records its id.
func (l *SoloLink) TodoFor(t *Task, b *brief.Brief) int {
	if l == nil || t.Todo != 0 {
		return t.Todo
	}
	if b.Todo != 0 {
		if _, err := l.Client.Call("todo_get", map[string]any{"project_id": l.ProjectID, "todo_id": b.Todo}); err == nil {
			t.Todo = b.Todo
			_ = t.Save()
			return t.Todo
		} else {
			ui.Warnf("solo todo %d: %v; creating a todo", b.Todo, err)
		}
	}
	body := fmt.Sprintf("Maestro task `%s` of run `%s`. Brief: `%s`.\n\n## Goal\n%s\n\n## Done\n%s\n", t.Task, t.Run, relOrAbs(t.Project, t.BriefPath), strings.TrimSpace(b.Goal), strings.TrimSpace(b.Done))
	id, err := l.Client.CreateTodo(l.ProjectID, t.Title, body, []string{"maestro"})
	if err != nil {
		ui.Warnf("solo todo: %v", err)
		return 0
	}
	t.Todo = id
	_ = t.Save()
	return id
}

// ReportComment posts the Report on the Task's todo: the full Report under
// 4 KB, else the status, changed files, checks, blocked list, and the path.
func (l *SoloLink) ReportComment(t *Task, rec *Record) {
	if l == nil || t.Todo == 0 {
		return
	}
	var w strings.Builder
	fmt.Fprintf(&w, "**Maestro** task `%s` attempt %d on `%s %s`: **%s** (%s)\n\n", t.Task, rec.Attempt, rec.Route.FamilyID, rec.Route.Effort, rec.Outcome.Outcome, rec.Outcome.Status)
	if rec.Outcome.ErrorText != "" {
		fmt.Fprintf(&w, "%s\n\n", rec.Outcome.ErrorText)
	}
	report, ok := rec.Report()
	switch {
	case ok && len(report.Raw) <= 4096:
		w.WriteString(report.Raw)
	case ok:
		fmt.Fprintf(&w, "STATUS: %s\n\nChanged: %s\n\nChecks: %d (%d failed)\n\nBlocked: %s\n\nReport: `%s`\n", report.Status, strings.Join(rec.Verification.Changed, ", "), len(rec.Verification.Checks), failed(rec), strings.Join(report.Blocked, "; "), filepath.Join(rec.Path, FileReport))
	default:
		fmt.Fprintf(&w, "No report. Folder: `%s`\n", rec.Path)
	}
	if err := l.Client.Comment(l.ProjectID, t.Todo, w.String()); err != nil {
		ui.Warnf("solo comment: %v", err)
		return
	}
	if rec.Outcome.Outcome == OutcomeVerified || rec.Outcome.Outcome == OutcomeAnswered || rec.Outcome.Outcome == OutcomeUnverified {
		_ = l.Client.CompleteTodo(l.ProjectID, t.Todo)
	}
}

func failed(rec *Record) int {
	n := 0
	for _, c := range rec.Verification.Checks {
		if !c.OK {
			n++
		}
	}
	return n
}

// Wake types a fixed Maestro message into the Orchestrator's session.
func (l *SoloLink) Wake(kind, id, title, harnessName, relayID string) {
	if l == nil || l.Orchestrator == 0 {
		return
	}
	verb := "maestro wait " + id
	switch kind {
	case "relay":
		verb = "maestro answer " + id + " allow|deny --relay " + relayID
	case "run end":
	default:
		return
	}
	body := fmt.Sprintf("Maestro %s: id=%q title=%q harness=%q. Run: %s", kind, id, title, harnessName, verb)
	if kind == "relay" {
		body = fmt.Sprintf("Maestro %s: id=%q title=%q harness=%q relay=%q. Run: %s", kind, id, title, harnessName, relayID, verb)
	}
	if err := l.Client.Timer(l.ProjectID, l.Orchestrator, time.Second, body); err != nil {
		ui.Warnf("solo wake-up: %v", err)
	}
}

// Only the Relay lock holder may consume a control folder's answer marker.
func readAnswer(controlFolder, relayID string) string {
	for _, ans := range []string{"allow", "deny"} {
		p := filepath.Join(controlFolder, "answer-"+ans)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		marker, err := os.CreateTemp(controlFolder, ".relay-marker-*")
		if err != nil {
			continue
		}
		_ = marker.Close()
		// Take this marker before reading it. A late answer can then publish
		// a new marker without the old reader removing that new answer.
		if err := os.Rename(p, marker.Name()); err != nil {
			_ = os.Remove(marker.Name())
			continue
		}
		b, err := os.ReadFile(marker.Name())
		_ = os.Remove(marker.Name())
		if err == nil && string(b) == relayID && relayID != "" {
			return ans
		}
	}
	return ""
}
