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

// TodoFor creates the Task's todo (Tasks are Solo todos) and records its id.
func (l *SoloLink) TodoFor(t *Task, b *brief.Brief) int {
	if l == nil || t.Todo != 0 {
		return t.Todo
	}
	body := fmt.Sprintf("Maestro task `%s` of run `%s`. Brief: `%s`.\n\n## Goal\n%s\n\n## Done\n%s\n", t.Task, t.Run, relOrAbs(t.Project, t.BriefPath), strings.TrimSpace(b.Goal), strings.TrimSpace(b.Done))
	id, err := l.Client.CreateTodo(l.ProjectID, t.Title, body, []string{"maestro", "ready-for-agent"})
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
	if rec.Outcome.Outcome == OutcomeVerified || rec.Outcome.Outcome == OutcomeAnswered {
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

// Wake types one line into the Orchestrator's session.
func (l *SoloLink) Wake(text string) {
	if l == nil || l.Orchestrator == 0 {
		return
	}
	if err := l.Client.Timer(l.ProjectID, l.Orchestrator, time.Second, text); err != nil {
		ui.Warnf("solo wake-up: %v", err)
	}
}

// startupPrompts are dialogs a TUI shows before it takes a prompt, with the
// keys that accept them. Claude's folder-trust dialog highlights "No, exit",
// so the answer is one Down arrow and Enter.
var startupPrompts = map[string][]struct {
	Marker string
	Keys   []byte
}{
	"claude": {{Marker: "Yes, I trust this folder", Keys: []byte("\x1b[B\r")}},
}

// promptMarkers is the per-Harness table of texts a TUI prints at a
// permission prompt (failure-handling.md section 7). The texts are the
// best reading of each TUI today; a miss means no Relay, never a false one.
var promptMarkers = map[string][]string{
	"claude": {"Do you want to proceed?", "Do you want to make this edit", "Do you want to create", "Yes, and don't ask again", "Allow once", "(y/n)"},
	"grok":   {"Allow", "Do you want to", "(y/n)", "approve"},
	"agy":    {"Allow", "approve", "(y/n)"},
	"codex":  {"Allow command?", "Approve", "(y/n)"},
}

// TabLane runs a Lane as an interactive Solo tab: spawn the preset with the
// Lane flags, type the Pointer, poll for result.md, relay prompts, and close
// the tab per close_tabs. It returns the Outcome the parser would have.
func (l *SoloLink) TabLane(env *Env, t *Task, h harness.Harness, command *harness.Command, pointer string, rec *Record, idle, wall time.Duration) (*harness.Outcome, string, error) {
	toolID := env.Roster.Harness[h.Name()].SoloTool
	if toolID == 0 {
		toolID = defaultSoloTool(env, h.Name())
	}
	if toolID == 0 {
		return nil, "", fmt.Errorf("no Solo agent tool for %s (set [harness.%s] solo_tool)", h.Name(), h.Name())
	}
	args := tabArgs(h.Name(), command, t)
	name := fmt.Sprintf("%s · %s", slug(t.Title), t.Route.FamilyID)
	pid, err := l.Client.Spawn(l.ProjectID, toolID, name, args)
	if err != nil {
		return nil, "", err
	}
	rec.SoloProcess = pid
	rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "tab", Text: fmt.Sprintf("solo process %d", pid)})
	_ = rec.Save()
	// Wait for the TUI to take input: alive and idle, with any startup dialog answered.
	if err := l.awaitReady(pid, h.Name(), rec); err != nil {
		_ = l.Client.CloseProcess(pid)
		return nil, "", err
	}
	if err := l.Client.SendInput(pid, pointer+"\n"); err != nil {
		_ = l.Client.CloseProcess(pid)
		return nil, "", err
	}
	start := time.Now()
	lastOutput := time.Now()
	lastTail := ""
	relayOpen := false
	var relayAt time.Time
	var relayTotal time.Duration
	answeredTail := ""
	nudged := false
	var nudgedAt time.Time
	reportPath := filepath.Join(t.Folder, FileReport)
	stop := func(reason string) {
		// Stop the Lane (Ctrl-C twice), then close the tab when it still runs.
		_ = l.Client.SendKeys(pid, []byte{3})
		time.Sleep(time.Second)
		_ = l.Client.SendKeys(pid, []byte{3})
		time.Sleep(3 * time.Second)
		if st, err := l.Client.Status(pid); err == nil && st.Running() {
			_ = l.Client.CloseProcess(pid)
		}
		rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "stop", Text: reason})
	}
	for {
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
		if err == nil && st.Status != "" && !st.Running() {
			// The tab ended without a Report.
			if st.Uptime < 30 {
				return &harness.Outcome{ErrorText: "early exit"}, "", nil
			}
			break
		}
		tail, _ := l.Client.Output(pid, 40)
		if tail != lastTail {
			lastTail = tail
			lastOutput = time.Now()
		}
		// Solo's own idle reading is the primary signal; the output diff is the backup.
		idleFor := time.Since(lastOutput)
		if err == nil && st.IdleSeconds > 0 && time.Duration(st.IdleSeconds*float64(time.Second)) < idleFor {
			idleFor = time.Duration(st.IdleSeconds * float64(time.Second))
		}
		// A prompt: a marker in the last lines AND 30 s idle, both required, and
		// never the same tail twice (an answered prompt stays on screen).
		q := lastLines(tail, 8)
		if !relayOpen && idleFor > 30*time.Second && hasMarker(h.Name(), q) && q != answeredTail {
			relayOpen = true
			relayAt = time.Now()
			rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "relay", Text: q})
			_ = rec.Save()
			ui.Eventf("relay", "task %s  %s asks:\n%s", t.Task, h.Name(), q)
			l.Wake(fmt.Sprintf("Maestro task %s (%s, %s) waits on a permission prompt. Answer with: maestro answer %s allow|deny. Prompt:\n%s", t.Task, h.Name(), t.Title, t.Task, q))
		}
		if relayOpen {
			if ans := readAnswer(t.Folder); ans != "" {
				key := []byte("\r")
				if ans == "deny" {
					key = []byte("\x1b")
				}
				_ = l.Client.SendKeys(pid, key)
				rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "relay-answer", Text: ans})
				relayOpen = false
				relayTotal += time.Since(relayAt)
				answeredTail = q
				lastOutput = time.Now()
			} else if time.Since(relayAt) > 10*time.Minute {
				_ = l.Client.SendKeys(pid, []byte("\x1b"))
				rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "relay-answer", Text: "denied: no answer"})
				relayOpen = false
				relayTotal += time.Since(relayAt)
				answeredTail = q
				lastOutput = time.Now()
			}
			time.Sleep(3 * time.Second)
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
		if wall > 0 && time.Since(start)-relayTotal > wall {
			stop("wall limit")
			return nil, "wall", nil
		}
		time.Sleep(5 * time.Second)
	}
	out := &harness.Outcome{}
	if raw, err := l.Client.Output(pid, 200); err == nil {
		out.Text = raw
	}
	return out, "", nil
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
		if rec.Outcome.Outcome != OutcomeVerified && rec.Outcome.Outcome != OutcomeAnswered {
			return
		}
	}
	_ = l.Client.CloseProcess(rec.SoloProcess)
}

// tabArgs turns the one-shot command's flags into extra_args for the preset:
// the Lane flags, then the model, effort, and permission flags. The prompt
// is typed afterwards, never passed as a positional.
func tabArgs(harnessName string, command *harness.Command, t *Task) []string {
	var args []string
	switch harnessName {
	case "claude":
		args = append(args, "--model", t.Route.Model)
		if t.Route.Effort != "" {
			args = append(args, "--effort", t.Route.Effort)
		}
		args = append(args, command.Flags...)
	case "grok":
		args = append(args, "--model", t.Route.Model, "--no-subagents")
		if t.Route.Effort != "" {
			args = append(args, "--reasoning-effort", t.Route.Effort)
		}
		args = append(args, command.Flags...)
	case "agy":
		model := t.Route.Model
		if t.Route.Effort != "" {
			model += "-" + t.Route.Effort
		}
		args = append(args, "--model", model)
		args = append(args, command.Flags...)
	}
	return args
}

func defaultSoloTool(env *Env, harnessName string) int {
	if rec, ok := env.Disc.Harness[harnessName]; ok && rec.SoloTool != 0 {
		return rec.SoloTool
	}
	return 0
}

func hasMarker(harnessName, tail string) bool {
	for _, m := range promptMarkers[harnessName] {
		if strings.Contains(tail, m) {
			return true
		}
	}
	return false
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// readAnswer consumes an answer marker written by maestro answer.
func readAnswer(folder string) string {
	for _, ans := range []string{"allow", "deny"} {
		p := filepath.Join(folder, "answer-"+ans)
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
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

// awaitReady waits up to 60 s for the tab to be alive and quiet, answering a
// known startup dialog on the way.
func (l *SoloLink) awaitReady(pid int, harnessName string, rec *Record) error {
	deadline := time.Now().Add(60 * time.Second)
	answered := false
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		st, err := l.Client.Status(pid)
		if err != nil {
			continue
		}
		if st.Status != "" && !st.Running() {
			return fmt.Errorf("the tab exited during startup")
		}
		tail, _ := l.Client.Output(pid, 30)
		if !answered {
			for _, sp := range startupPrompts[harnessName] {
				if strings.Contains(tail, sp.Marker) {
					_ = l.Client.SendKeys(pid, sp.Keys)
					rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "startup", Text: "answered: " + sp.Marker})
					answered = true
					time.Sleep(2 * time.Second)
					break
				}
			}
			if answered {
				continue
			}
		}
		if st.Uptime > 3 && st.IdleSeconds >= 2 {
			return nil
		}
	}
	return fmt.Errorf("the tab did not become ready in 60 s")
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
