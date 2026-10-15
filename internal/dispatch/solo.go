package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/harness"
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

// TabLane runs a Lane as an interactive Solo tab: spawn the preset with the
// Lane flags and Pointer, poll for result.md, and relay prompts. It returns the Outcome the parser would have.
func (l *SoloLink) TabLane(env *Env, t *Task, h harness.Harness, command *harness.Command, rec *Record, idle, wall time.Duration) (out *harness.Outcome, timer string, err error) {
	toolID := l.toolID(env, h.Name())
	if toolID == 0 {
		return nil, "", fmt.Errorf("no Solo agent tool for %s (set [harness.%s] solo_tool)", h.Name(), h.Name())
	}
	args := tabArgs(command)
	name := fmt.Sprintf("%s · %s", slug(t.Title), t.Route.FamilyID)
	launched := time.Now()
	pid, err := l.Client.Spawn(l.ProjectID, toolID, name, args)
	if err != nil {
		return nil, "", err
	}
	rec.SoloProcess = pid
	rec.Events = append(rec.Events, Event{At: launched, Kind: "tab", Text: fmt.Sprintf("solo process %d", pid)})
	_ = rec.Save()
	output := &tabOutput{}
	defer func() {
		l.saveTabLog(pid, t.Folder)
		if out == nil {
			out = &harness.Outcome{}
		}
		out.TabWorked = output.worked
		if raw, err := os.ReadFile(filepath.Join(t.Folder, "tab.log")); err == nil {
			out.Text = string(raw)
			out.Signals = append(out.Signals, harness.ClassifyText(h.Name(), out.Text))
		}
	}()
	start := time.Now()
	lastOutput := time.Now()
	relays := &relayTracker{}
	nudged := false
	var nudgedAt time.Time
	statusErrors := 0
	reportPath := filepath.Join(t.Folder, FileReport)
	stop := func(reason string) {
		l.saveTabLog(pid, t.Folder)
		if st, err := l.Client.Status(pid); err == nil && !st.Running() {
			rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "stop", Text: reason})
			return
		}
		// Stop the Lane; CloseTab applies close_tabs after the Report is judged.
		_ = l.Client.SendKeys(pid, []byte{3})
		time.Sleep(time.Second)
		_ = l.Client.SendKeys(pid, []byte{3})
		time.Sleep(3 * time.Second)
		rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "stop", Text: reason})
	}
	for {
		wasOpen := relays.open()
		relays.poll(env, t, rec, h.Name(), time.Now())
		if wasOpen || relays.open() {
			lastOutput = time.Now()
		}
		if reportStable(reportPath) {
			// The file is written and the Lane is quiet: judge it.
			if st, err := l.Client.Status(pid); err != nil || !st.Running() || st.IdleSeconds >= 5 {
				break
			}
		}
		select {
		case <-t.Cancel:
			stop("cancelled")
			return nil, "cancelled", nil
		default:
		}
		st, err := l.Client.Status(pid)
		if tail, outputErr := l.Client.Output(pid, 40); outputErr == nil && output.observe(tail) {
			lastOutput = time.Now()
		}
		if err != nil {
			// A closed tab is gone from Solo: three answers in a row say so.
			if statusErrors++; statusErrors >= 3 {
				rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "stop", Text: "tab closed: " + err.Error()})
				break
			}
		} else {
			statusErrors = 0
		}
		if err == nil && st.Status != "" && !st.Running() {
			// The tab ended without a Report.
			if st.Uptime < 30 {
				stop("early exit")
				return &harness.Outcome{ErrorText: "early exit"}, "", nil
			}
			break
		}
		// Solo's own idle reading is the primary signal; the output diff is the backup.
		idleFor := time.Since(lastOutput)
		if err == nil && st.IdleSeconds > 0 && time.Duration(st.IdleSeconds*float64(time.Second)) < idleFor {
			idleFor = time.Duration(st.IdleSeconds * float64(time.Second))
		}
		if relays.open() {
			time.Sleep(time.Second)
			continue
		}
		if idle > 0 && idleFor > idle {
			if !nudged {
				// One nudge, then two more minutes; never a second Idle period.
				nudged = true
				nudgedAt = time.Now()
				rec.Outcome.Nudged = true
				_ = l.Client.SendInput(pid, "Write your report to `result.md` now, as the brief says.\n")
				continue
			}
			if time.Since(nudgedAt) > 2*time.Minute {
				stop("idle limit")
				return nil, "idle", nil
			}
		}
		// The Wall limit does not count time spent in a Relay.
		if wall > 0 && time.Since(start)-relays.elapsed(time.Now()) > wall {
			stop("wall limit")
			return nil, "wall", nil
		}
		time.Sleep(5 * time.Second)
	}
	relays.poll(env, t, rec, h.Name(), time.Now())
	select {
	case <-t.Cancel:
		stop("cancelled")
		return nil, "cancelled", nil
	default:
	}
	stop("report ready or tab ended")
	return &harness.Outcome{}, "", nil
}

// tabOutput compares output without reading or interpreting prompt text.
type tabOutput struct {
	seen   bool
	last   string
	worked bool
}

func (o *tabOutput) observe(text string) bool {
	if !o.seen {
		if strings.TrimSpace(text) == "" {
			return false
		}
		o.seen, o.last = true, text
		return true
	}
	if text == o.last {
		return false
	}
	o.last, o.worked = text, true
	return true
}

// CloseTab applies close_tabs after the Dispatch is judged.
func (l *SoloLink) CloseTab(env *Env, rec *Record) {
	if l == nil || rec.SoloProcess == 0 {
		return
	}
	switch env.Roster.Concurrency.CloseTabs {
	case "never":
		return
	case "always":
	default:
		if rec.Outcome.Outcome != OutcomeVerified && rec.Outcome.Outcome != OutcomeAnswered && rec.Outcome.Outcome != OutcomeUnverified {
			return
		}
	}
	_ = l.Client.CloseProcess(rec.SoloProcess)
}

// tabArgs uses the Harness's interactive shape as Solo preset arguments.
func tabArgs(command *harness.Command) []string {
	return append([]string(nil), command.Cmd.Args[1:]...)
}

func (l *SoloLink) toolID(env *Env, harnessName string) int {
	if id := env.Roster.Harness[harnessName].SoloTool; id != 0 {
		return id
	}
	if env.Disc != nil {
		return env.Disc.Harness[harnessName].SoloTool
	}
	return 0
}

func (l *SoloLink) saveTabLog(pid int, folder string) {
	raw, err := l.Client.Output(pid, 200)
	if err != nil {
		ui.Warnf("tab output: %v", err)
		return
	}
	if err := os.WriteFile(filepath.Join(folder, "tab.log"), []byte(raw), 0o644); err != nil {
		ui.Warnf("tab log: %v", err)
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

func slug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	last := '-'
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			last = r
		} else if last != '-' {
			b.WriteRune('-')
			last = '-'
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}

// reportStable reports a non-empty result.md whose size has not changed for 3 s.
func reportStable(path string) bool {
	st1, err := os.Stat(path)
	if err != nil || st1.Size() == 0 {
		return false
	}
	time.Sleep(3 * time.Second)
	st2, err := os.Stat(path)
	return err == nil && st2.Size() == st1.Size() && st2.ModTime().Equal(st1.ModTime())
}
