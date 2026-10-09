package dispatch

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/jalendport/maestro/internal/verify"
)

// Env is what every Dispatch reads: the Roster, the discovery cache, the
// adapters, the project file, and the project root.
type Env struct {
	Roster  *roster.Roster
	Disc    *discovery.Cache
	Reg     *harness.Registry
	Project *config.Project
	Root    string
	Budget  *budget.File
	Solo    *SoloLink // nil outside Solo
}

// NewRouter builds the Router over this Env.
func (env *Env) NewRouter() *router.Router {
	return &router.Router{Roster: env.Roster, Disc: env.Disc, Reg: env.Reg, Budget: env.Budget}
}

// RefreshBudget probes every Account whose reading is stale and has a probe
// (Claude: Haiku; Codex: app-server), before a Route (budget-model.md section 8).
func (env *Env) RefreshBudget() {
	if env.Budget == nil {
		return
	}
	rt := env.NewRouter()
	changed := false
	for id, acct := range env.Roster.Accounts {
		v := rt.View(id)
		if !(v.State == "stale" || v.State == "unknown") {
			continue
		}
		h, _ := env.Reg.Get(acct.Harness)
		if h == nil || h.Bin() == "" {
			continue
		}
		switch acct.Harness {
		case "claude":
			if v.State == "unknown" && len(env.Budget.Accounts) > 0 && env.Budget.Accounts[id] != nil {
				continue // an expired reading; the next harvest refreshes it
			}
			if ws, err := budget.ProbeClaude(h.Bin(), harness.Env(env.Roster.StripEnv("claude"), map[string]string{"CLAUDE_CODE_DISABLE_ADVISOR_TOOL": "1"})); err == nil {
				env.Budget.Record(id, ws, budget.SourceProbe)
				changed = true
				ui.Eventf("budget", "%s probed: %s", id, describe(ws))
			} else {
				ui.Warnf("budget probe %s: %v", id, err)
			}
		case "codex":
			if cl, err := budget.ReadCodex(h.Bin()); err == nil {
				env.Budget.Record(id, cl.Windows, budget.SourceProbe)
				env.Budget.Accounts[id].Credits = cl.Credits
				env.Budget.Accounts[id].Plan = cl.Plan
				changed = true
				ui.Eventf("budget", "%s read: %s", id, describe(cl.Windows))
			} else {
				ui.Warnf("budget read %s: %v", id, err)
			}
		}
	}
	if changed {
		_ = env.Budget.Save()
	}
}

func describe(ws []harness.Window) string {
	var parts []string
	for _, w := range ws {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", w.Kind, w.Used*100))
	}
	return strings.Join(parts, ", ")
}

// harvest stores the windows a Dispatch reported, at no extra cost.
func (env *Env) harvest(account string, ws []harness.Window) {
	if env.Budget == nil || len(ws) == 0 {
		return
	}
	env.Budget.Record(account, ws, budget.SourceHarvest)
	_ = env.Budget.Save()
}

// confirmLimit checks a limit message against a fresh reading before the
// Account blocks (failure-handling.md section 6). It returns true when the
// limit stands; a reading under the block point downgrades it.
func (env *Env) confirmLimit(account, harnessName string) (bool, string) {
	if env.Budget == nil {
		return true, "no budget file"
	}
	h, _ := env.Reg.Get(harnessName)
	until := time.Now().Add(time.Hour)
	reason := "limit hit"
	switch harnessName {
	case "claude":
		if ws, err := budget.ProbeClaude(h.Bin(), harness.Env(env.Roster.StripEnv("claude"), map[string]string{"CLAUDE_CODE_DISABLE_ADVISOR_TOOL": "1"})); err == nil {
			env.Budget.Record(account, ws, budget.SourceProbe)
			if v := env.NewRouter().View(account); v.State != "blocked" {
				_ = env.Budget.Save()
				return false, "probe shows headroom: " + describe(ws)
			}
			until = nearestReset(ws, until)
		}
	case "codex":
		if cl, err := budget.ReadCodex(h.Bin()); err == nil {
			env.Budget.Record(account, cl.Windows, budget.SourceProbe)
			if v := env.NewRouter().View(account); v.State != "blocked" && cl.Reached == "" {
				_ = env.Budget.Save()
				return false, "app-server shows headroom: " + describe(cl.Windows)
			}
			until = nearestReset(cl.Windows, until)
		}
	default:
		reason = "limit hit (no reading; trusted for 1 h)"
	}
	env.Budget.Block(account, until, reason)
	_ = env.Budget.Save()
	return true, reason
}

func nearestReset(ws []harness.Window, fallback time.Time) time.Time {
	best := fallback
	for _, w := range ws {
		if w.Used >= 0.9 && !w.ResetsAt.IsZero() && w.ResetsAt.After(time.Now()) {
			if best == fallback || w.ResetsAt.Before(best) {
				best = w.ResetsAt
			}
		}
	}
	return best
}

// InSolo reports whether this process runs inside a Solo tab.
func InSolo() bool { return os.Getenv("SOLO_PROCESS_ID") != "" }

// Prepare routes one Brief and lays out its Run folder. It returns the Task
// to execute, inline or in a detached supervisor.
func Prepare(env *Env, b *brief.Brief, req router.Request, runID, taskID string) (*Task, error) {
	return PrepareIn(env, b, req, runID, taskID, "")
}

// PrepareIn is Prepare with the Run directory given (Councils keep their
// Seats under .agents/councils/<slug>/round-N/ instead of the runs folder).
func PrepareIn(env *Env, b *brief.Brief, req router.Request, runID, taskID, runDir string) (*Task, error) {
	env.RefreshBudget()
	rt := env.NewRouter()
	req.Brief = b
	req.Project = env.Project
	req.Access = b.EffectiveAccess()
	req.InSolo = InSolo()
	route, err := rt.Route(req)
	if err != nil {
		return nil, err
	}
	if runID == "" {
		runID = NextRunID(env.Root)
	}
	if runDir == "" {
		runDir = filepath.Join(config.RunsDir(env.Root), runID)
	}
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	if taskID == "" {
		taskID = NewTaskID(runDir)
	}
	folder := filepath.Join(runDir, taskID)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return nil, err
	}
	var dirs []string
	if env.Project != nil {
		for _, d := range env.Project.Dirs {
			dirs = append(dirs, config.ExpandHome(d))
		}
	}
	for _, d := range b.Dirs {
		dirs = append(dirs, config.ExpandHome(d))
	}
	wall := env.Roster.WallLimit(route.Tier).Duration
	if d := b.WallLimitDuration(); d > wall {
		wall = d
	}
	t := &Task{
		Project: env.Root, Run: runID, Task: taskID, BriefPath: b.Path, Title: b.Title,
		Category: route.Category, Access: req.Access, Commit: b.WantsCommit(), Touch: b.Touch, Dirs: dirs,
		Checks: b.Checks, WallLimit: wall.String(), IdleLimit: env.Roster.Failure.IdleLimit.Duration.String(),
		Route: route, Folder: folder, ClaudeAuto: env.Project != nil && env.Project.ClaudeAuto, InSolo: InSolo(),
		MaxAttempt: 2, Council: req.Seat != "",
	}
	if err := t.Save(); err != nil {
		return nil, err
	}
	ui.Eventf("route", "%s", route.ReasonLine)
	if route.OverrideWarning != "" {
		ui.Warnf("%s", route.OverrideWarning)
	}
	if route.GapFlag {
		ui.Warnf("judgment level estimate %s is two levels from the router's %s", route.LevelEstimate, route.Level)
	}
	return t, nil
}

// Execute runs the Task's attempts, records each, and appends the ledger row
// for the final one. Lane and Transient errors retry once on the same Lane.
func Execute(env *Env, t *Task) (*Record, error) {
	b, err := brief.Parse(t.BriefPath)
	if err != nil {
		return nil, err
	}
	// task.json carries the family id only; a reloaded Task gets its family back here.
	if t.Route.Family == nil {
		f, ok := env.Roster.Family(t.Route.FamilyID)
		if !ok {
			return nil, fmt.Errorf("task %s: family %q is not in the roster", t.Task, t.Route.FamilyID)
		}
		t.Route.Family = f
	}
	stopAwake := keepAwake(env.Roster.KeepAwake())
	defer stopAwake()

	var rec *Record
	var retry *brief.Retry
	first := t.FirstAttempt
	if first < 1 {
		first = 1
	}
	if first > 1 {
		retry = &brief.Retry{Attempt: first, Reason: "earlier attempt failed"}
	}
	if !t.Council {
		env.Solo.TodoFor(t, b)
	}
	for attempt := first; attempt <= t.MaxAttempt; attempt++ {
		rec = runOnce(env, t, b, attempt, retry)
		if err := ledger.Append(rec); err != nil {
			ui.Warnf("ledger: %v", err)
		}
		if !t.Council {
			env.Solo.ReportComment(t, rec)
		}
		env.Solo.CloseTab(env, rec)
		wait, again := retryWait(rec.Outcome.FailureClass)
		if !again || attempt == t.MaxAttempt {
			break
		}
		ui.Eventf("retry", "task %s  %s: %s; retrying on %s in %s", t.Task, rec.Outcome.FailureClass, firstLine(rec.Outcome.ErrorText), t.Route.FamilyID, wait)
		archiveAttempt(t.Folder, attempt)
		retry = &brief.Retry{Attempt: attempt + 1, Reason: rec.Outcome.FailureClass}
		select {
		case <-time.After(wait):
		case <-t.Cancel:
			rec.Outcome.Outcome = OutcomeCancelled
			_ = rec.Save()
			break
		}
	}
	return rec, nil
}

func retryWait(class string) (time.Duration, bool) {
	switch class {
	case FailLane:
		return 5 * time.Second, true
	case FailTransient:
		return 30 * time.Second, true
	}
	return 0, false
}

// archiveAttempt moves a failed attempt's files under attempts/<n>/ so the
// retry starts with a clean folder and nothing is lost.
func archiveAttempt(folder string, n int) {
	dst := filepath.Join(folder, FileAttempts, strconv.Itoa(n))
	_ = os.MkdirAll(dst, 0o755)
	for _, f := range []string{FileBrief, FileReport, FileOutput, FileStderr, FileRecord} {
		_ = os.Rename(filepath.Join(folder, f), filepath.Join(dst, f))
	}
}

func runOnce(env *Env, t *Task, b *brief.Brief, attempt int, retry *brief.Retry) *Record {
	rec := &Record{
		Kind: "dispatch", Project: t.Project, Run: t.Run, Task: t.Task, Attempt: attempt, State: "running",
		Pid: os.Getpid(), StartedAt: time.Now(), Path: t.Folder, Route: t.Route,
		TaskInfo:     TaskInfo{Title: t.Title, Category: t.Category, LevelEstimate: string(t.Route.LevelEstimate), Access: t.Access, Commit: t.Commit, WallLimit: t.WallLimit},
		Permission:   Permission{Enforcement: t.Route.Enforcement, Denials: []harness.Denial{}, Blocked: []string{}, Relays: []json.RawMessage{}},
		Verification: Verification{Changed: []string{}, TouchViolations: []string{}, Checks: []verify.CheckResult{}},
	}
	fail := func(class, text string) *Record {
		rec.Outcome.FailureClass = class
		rec.Outcome.Outcome = OutcomeFailed
		rec.Outcome.ErrorText = cut(text, 500)
		rec.Outcome.Status = ""
		finish(rec)
		ui.Eventf("failed", "task %s  %s: %s", t.Task, class, firstLine(text))
		return rec
	}

	h, ok := env.Reg.Get(t.Route.Harness)
	if !ok {
		return fail(FailLane, "unknown harness "+t.Route.Harness)
	}
	reportPath := filepath.Join(t.Folder, FileReport)
	briefRel, _ := filepath.Rel(t.Project, filepath.Join(t.Folder, FileBrief))
	reportRel, _ := filepath.Rel(t.Project, reportPath)

	// Checks are verification commands for edits; a read Lane cannot run them.
	projectChecks := []string{}
	briefChecks := []string{}
	if t.Access == brief.AccessEdit {
		if env.Project != nil {
			projectChecks = env.Project.Checks
		}
		briefChecks = t.Checks
	}
	allChecks := append(append([]string{}, projectChecks...), briefChecks...)

	launch := harness.Launch{
		Model: t.Route.Model, Effort: t.Route.Effort, Access: t.Access, Root: t.Project, Dirs: t.Dirs,
		AllowedCommands: allChecks, AllowCommit: t.Commit, RunFolder: t.Folder, ReportPath: reportPath,
		Pointer: brief.Pointer(briefRel), ClaudeAuto: t.ClaudeAuto, InSolo: t.InSolo, WallLimit: wallOf(t.WallLimit),
		Env: harness.Env(env.Roster.StripEnv(t.Route.Harness), map[string]string{
			"CLAUDE_CODE_DISABLE_ADVISOR_TOOL": "1",
			"GROK_CLAUDE_HOOKS_ENABLED":        "false",
			"GROK_CLAUDE_SKILLS_ENABLED":       "false",
			"GROK_CLAUDE_RULES_ENABLED":        "false",
			"GROK_CLAUDE_AGENTS_ENABLED":       "false",
			"MAESTRO_LANE":                     "1",
			"MAESTRO_RUN":                      t.Run,
			"MAESTRO_TASK":                     t.Task,
		}),
	}
	command, err := h.Build(launch)
	if err != nil {
		return fail(FailLane, err.Error())
	}
	rec.Permission.Flags = command.Flags
	rec.Outcome.ReportByText = command.ReportByText

	rendered := brief.Render(b, brief.RenderOpts{
		TaskID: t.Task, RunID: t.Run, Access: t.Access, ProjectChecks: projectChecks, BriefChecks: briefChecks,
		ReportPath: reportRel, Retry: retry, ReportByMessage: command.ReportByText, Council: t.Council,
	})
	if err := os.WriteFile(filepath.Join(t.Folder, FileBrief), []byte(rendered), 0o644); err != nil {
		return fail(FailLane, err.Error())
	}
	// One baseline per Task, taken before the first attempt and shared by every
	// retry and re-Route, so a failed attempt's edits never escape the scope
	// guard or the gate (found by the first dogfood Council).
	snap, err := verify.LoadSnapshot(filepath.Join(t.Folder, FileBaseline))
	if err != nil {
		snap = verify.Take(t.Project)
		_ = snap.SaveTo(filepath.Join(t.Folder, FileBaseline))
	}
	rec.Verification.StartCommit = snap.Commit

	// In Solo, Claude, Grok, and agy Lanes are interactive tabs; Codex stays
	// one-shot until its TUI in Solo is re-verified; Council Seats are always one-shot.
	if env.Solo != nil && !t.Council && h.Name() != "codex" {
		return runTab(env, t, h, command, launch, rec, reportPath, allChecks, b)
	}

	// Launch.
	cmd := command.Cmd
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(FailLane, err.Error())
	}
	stderrFile, _ := os.Create(filepath.Join(t.Folder, FileStderr))
	defer stderrFile.Close()
	cmd.Stderr = stderrFile
	var stdin io.WriteCloser
	if command.PromptStdin {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return fail(FailLane, err.Error())
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fail(FailLane, "process did not start: "+err.Error())
	}
	rec.Pid = cmd.Process.Pid
	rec.Events = append(rec.Events, Event{At: time.Now(), Kind: "start", Text: fmt.Sprintf("%s pid %d", h.Name(), cmd.Process.Pid)})
	_ = rec.Save()
	ui.Eventf("dispatch", "task %s  %s %s %s  %s  wall %s  → %s", t.Task, h.Name(), t.Route.Model, t.Route.Effort, t.Access, t.WallLimit, relOrAbs(t.Project, t.Folder))
	if stdin != nil {
		go func() {
			_, _ = io.WriteString(stdin, launch.Pointer+"\n")
			stdin.Close()
		}()
	}

	// Pump the stream to output.jsonl and the parser; track the last output time.
	parser := h.NewParser()
	outFile, _ := os.Create(filepath.Join(t.Folder, FileOutput))
	var mu sync.Mutex
	lastOutput := time.Now()
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for sc.Scan() {
			line := sc.Bytes()
			if outFile != nil {
				outFile.Write(line)
				outFile.Write([]byte{'\n'})
			}
			if parser.Line(line) {
				mu.Lock()
				lastOutput = time.Now()
				mu.Unlock()
			}
		}
	}()

	// Timers: Idle and Wall, Maestro-owned (failure-handling.md section 3).
	idle, _ := time.ParseDuration(t.IdleLimit)
	wall, _ := time.ParseDuration(t.WallLimit)
	waitDone := make(chan error, 1)
	go func() { <-pumpDone; waitDone <- cmd.Wait() }()
	var timer string
	var waitErr error
	ticker := time.NewTicker(time.Second)
	lastTick := time.Now()
	cancelled := false
loop:
	for {
		select {
		case waitErr = <-waitDone:
			break loop
		case <-t.Cancel:
			cancelled = true
			ui.Eventf("cancel", "task %s  stopping %s", t.Task, h.Name())
			terminate(cmd)
			waitErr = <-waitDone
			break loop
		case now := <-ticker.C:
			// A clock jump after Mac sleep: a live Dispatch gets a fresh Idle timer.
			if jump := now.Sub(lastTick); jump > 2*time.Minute {
				rec.Outcome.SleepJumpS = jump.Seconds()
				mu.Lock()
				lastOutput = now
				mu.Unlock()
				rec.Events = append(rec.Events, Event{At: now, Kind: "wake", Text: fmt.Sprintf("clock jumped %s", jump.Round(time.Second))})
			}
			lastTick = now
			mu.Lock()
			idleFor := now.Sub(lastOutput)
			mu.Unlock()
			switch {
			case wall > 0 && now.Sub(rec.StartedAt) > wall:
				timer = "wall"
			case idle > 0 && idleFor > idle:
				timer = "idle"
			}
			if timer != "" {
				ui.Eventf("stall", "task %s  %s limit passed; stopping %s", t.Task, timer, h.Name())
				terminate(cmd)
				waitErr = <-waitDone
				break loop
			}
		}
	}
	ticker.Stop()
	if outFile != nil {
		outFile.Close()
	}
	exitCode := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	rec.Outcome.ExitCode = exitCode
	out := parser.Outcome()
	rec.SessionID = out.SessionID
	rec.Outcome.ModelRan = out.ModelRan
	if rec.Outcome.ModelRan == "" {
		rec.Outcome.ModelRan = t.Route.Model
	}
	rec.Budget.Tokens = out.Tokens
	rec.Budget.CostUSD = out.CostUSD
	if rec.Budget.CostUSD == nil {
		rec.Budget.CostUSD = priced(env, rec.Outcome.ModelRan, t.Route.Model, out.Tokens)
	}
	rec.Budget.Windows = out.Windows
	rec.Permission.Denials = append(rec.Permission.Denials, out.Denials...)
	if t.Route.Budget != nil {
		rec.Budget.Reading = &Reading{Used: t.Route.Budget.Used, Source: t.Route.Budget.Source, State: t.Route.Budget.State}
		if ts, err := time.Parse(time.RFC3339, t.Route.Budget.ReadAt); err == nil {
			rec.Budget.Reading.ReadAt = ts
		}
	}
	rec.Budget.Warning = t.Route.BudgetWarning
	// Harvest: Claude reports its windows in the stream; Codex writes them to its rollout file.
	switch t.Route.Harness {
	case "claude":
		env.harvest(t.Route.Account, out.Windows)
	case "codex":
		if out.SessionID != "" {
			if ws, err := budget.HarvestCodexRollout(out.SessionID); err == nil {
				rec.Budget.Windows = ws
				env.harvest(t.Route.Account, ws)
			}
		}
	}

	if cancelled {
		rec.Outcome.Outcome = OutcomeCancelled
		finish(rec)
		return rec
	}
	if timer != "" {
		rec.Outcome.Timer = timer
		rec.Outcome.FailureClass = FailStall
		rec.Outcome.Outcome = OutcomeTimedOut
		finish(rec)
		return rec
	}

	// Collect the Report.
	report, found := rec.Report()
	if !found || strings.TrimSpace(report.Raw) == "" {
		if strings.TrimSpace(out.Text) != "" {
			// The Lane left no file; its final text is the Report. A status
			// line in that text counts; without one the Report is unshaped.
			report = brief.ParseReport(out.Text)
			text := strings.TrimSpace(out.Text) + "\n"
			if report.Status == brief.StatusUnshaped {
				text += "\nSTATUS: unshaped\n"
			}
			_ = os.WriteFile(reportPath, []byte(text), 0o644)
			rec.Outcome.ReportByText = true
			found = true
		}
	} else if command.ReportByText && report.Status == brief.StatusUnshaped {
		// Codex -o wrote the last message; the Lane may still have shaped it.
		report = brief.ParseReport(report.Raw)
	}
	for _, s := range out.Signals {
		if s.Class == "limit" {
			stands, note := env.confirmLimit(t.Route.Account, t.Route.Harness)
			rec.Outcome.LimitConfirmation = note
			if !stands {
				return fail(FailTransient, s.Text+" (downgraded: "+note+")")
			}
			return fail(FailLimit, s.Text)
		}
	}
	if !found {
		class := FailTransient
		text := out.ErrorText
		if text == "" {
			text = tail(filepath.Join(t.Folder, FileStderr), 500)
		}
		for _, s := range out.Signals {
			if s.Class == "lane" {
				class = FailLane
			}
		}
		if exitCode != 0 && text == "" {
			text = fmt.Sprintf("%s exited %d with no report", h.Name(), exitCode)
		}
		if exitCode == 0 && text == "" {
			text = fmt.Sprintf("%s exited 0 with no report and no text", h.Name())
		}
		return fail(class, text)
	}
	if out.IsError && report.Status == brief.StatusUnshaped {
		class := FailTransient
		for _, s := range out.Signals {
			if s.Class == "lane" {
				class = FailLane
			}
		}
		return fail(class, out.ErrorText)
	}

	if t.Council && report.Position != "" && report.Status == brief.StatusUnshaped {
		// A Seat ends with POSITION, not STATUS; a position line is a shaped Report.
		report.Status = brief.StatusDone
	}
	return judge(env, t, rec, report, allChecks, b)
}

// judge applies the Report status, the scope guard, the checks, and the gate.
func judge(env *Env, t *Task, rec *Record, report *brief.Report, allChecks []string, b *brief.Brief) *Record {
	snap, err := verify.LoadSnapshot(filepath.Join(t.Folder, FileBaseline))
	if err != nil {
		snap = verify.Snapshot{Commit: rec.Verification.StartCommit}
	}
	rec.Outcome.Status = report.Status
	rec.Permission.Blocked = report.Blocked
	switch report.Status {
	case brief.StatusBlocked:
		rec.Outcome.Outcome = OutcomeBlocked
		rec.Outcome.FailureClass = FailBad
		finish(rec)
		ui.Eventf("blocked", "task %s  %d blocked item(s)", t.Task, len(report.Blocked))
		return rec
	case brief.StatusPartial:
		rec.Outcome.Outcome = OutcomeFailed
		rec.Outcome.FailureClass = FailBad
	}

	// Verification: changed files, scope guard, commit range, checks.
	runRel, _ := filepath.Rel(t.Project, config.RunsDir(t.Project))
	rec.Verification.Changed = without(snap.Changed(t.Project), runRel)
	// A read Lane's Enforcement keeps it from writing; tree changes seen during a
	// read Dispatch belong to the edit Dispatch beside it, so they are recorded
	// but never counted as violations.
	if t.Access == brief.AccessEdit {
		rec.Verification.TouchViolations = verify.ScopeViolations(rec.Verification.Changed, t.Touch, runRel)
		if t.Commit {
			rec.Verification.CommitRange = verify.CommitRange(t.Project, rec.Verification.StartCommit)
		}
		if len(allChecks) > 0 {
			ui.Eventf("verify", "task %s  running %d check(s)", t.Task, len(allChecks))
			rec.Verification.Checks = verify.RunChecks(t.Project, allChecks, 10*time.Minute)
		}
	}
	if rec.Verification.TouchViolations == nil {
		rec.Verification.TouchViolations = []string{}
	}
	if rec.Verification.Checks == nil {
		rec.Verification.Checks = []verify.CheckResult{}
	}
	if rec.Verification.Changed == nil {
		rec.Verification.Changed = []string{}
	}
	failedChecks := 0
	for _, c := range rec.Verification.Checks {
		if !c.OK {
			failedChecks++
		}
	}
	if rec.Outcome.Outcome == "" {
		switch {
		case len(rec.Verification.TouchViolations) > 0:
			rec.Outcome.Outcome = OutcomeFailed
			rec.Outcome.FailureClass = FailBad
			rec.Outcome.ErrorText = "scope: " + strings.Join(rec.Verification.TouchViolations, ", ")
		case failedChecks > 0:
			rec.Outcome.Outcome = OutcomeFailed
			rec.Outcome.FailureClass = FailBad
			rec.Outcome.ErrorText = fmt.Sprintf("%d check(s) failed", failedChecks)
		case t.Access == brief.AccessRead:
			rec.Outcome.Outcome = OutcomeAnswered
		default:
			// Deterministic checks passed: the Jev gate judges the diff against the Brief.
			gate := runGate(env, t, rec, b.Goal, b.Done)
			rec.Verification.Gate = gate.raw()
			switch {
			case gate.Skipped != "" && len(allChecks) == 0:
				rec.Outcome.Outcome = OutcomeUnverified
			case gate.Skipped != "":
				rec.Outcome.Outcome = OutcomeVerified
			case gate.Satisfies >= env.Roster.Threshold("satisfies") && len(allChecks) > 0:
				rec.Outcome.Outcome = OutcomeVerified
			case gate.Satisfies >= env.Roster.Threshold("satisfies"):
				rec.Outcome.Outcome = OutcomeUnverified
			case gate.Verdict == "pass":
				// Checks pass and Jev sees no fault, but satisfies sits under the
				// close threshold: not closed, not failed. The Orchestrator decides.
				rec.Outcome.Outcome = OutcomeUnverified
				rec.Outcome.ErrorText = fmt.Sprintf("gate: satisfies %.2f under %.2f with verdict pass", gate.Satisfies, env.Roster.Threshold("satisfies"))
			default:
				rec.Outcome.Outcome = OutcomeFailed
				rec.Outcome.FailureClass = FailBad
				rec.Outcome.ErrorText = fmt.Sprintf("gate: satisfies %.2f, verdict %s, risk %s", gate.Satisfies, gate.Verdict, gate.Risk)
			}
			if gate.Skipped == "" {
				ui.Eventf("gate", "task %s  satisfies %.2f  verdict %s  risk %s", t.Task, gate.Satisfies, gate.Verdict, gate.Risk)
			}
		}
	}
	finish(rec)
	ui.Eventf(rec.Outcome.Outcome, "task %s  status %s  %s", t.Task, rec.Outcome.Status, rec.StartedAt.Format("15:04"))
	return rec
}

func finish(rec *Record) {
	rec.EndedAt = time.Now()
	rec.DurationS = rec.EndedAt.Sub(rec.StartedAt).Seconds()
	rec.State = "ended"
	rec.Pid = 0
	_ = rec.Save()
}

// terminate sends SIGTERM to the process group, waits 15 s, then SIGKILL.
func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid := -cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil || syscall.Kill(cmd.Process.Pid, 0) != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(pgid, syscall.SIGKILL)
}

// keepAwake starts caffeinate -s tied to this process; no-op when off or absent.
func keepAwake(on bool) func() {
	if !on {
		return func() {}
	}
	path, err := exec.LookPath("caffeinate")
	if err != nil {
		return func() {}
	}
	c := exec.Command(path, "-s", "-w", strconv.Itoa(os.Getpid()))
	if err := c.Start(); err != nil {
		return func() {}
	}
	return func() { _ = c.Process.Kill(); _, _ = c.Process.Wait() }
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

func without(paths []string, prefix string) []string {
	var out []string
	for _, p := range paths {
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func relOrAbs(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

// priced prices tokens with the embedded table when the Harness reports no cost.
func priced(env *Env, modelRan, requested string, tk harness.Tokens) *float64 {
	id := modelRan
	if id == "" {
		id = requested
	}
	if alias, ok := env.Roster.Aliases[id]; ok {
		id = alias
	}
	p, ok := env.Roster.Prices[id]
	if !ok || (tk.Input == nil && tk.Output == nil) {
		return nil
	}
	n := func(x *int) float64 {
		if x == nil {
			return 0
		}
		return float64(*x)
	}
	cost := (n(tk.Input)-n(tk.CachedInput))*p.Input/1e6 + n(tk.CachedInput)*p.CachedInput/1e6 + n(tk.Output)*p.Output/1e6
	if cost < 0 {
		cost = n(tk.Input)*p.Input/1e6 + n(tk.Output)*p.Output/1e6
	}
	return &cost
}

func wallOf(s string) time.Duration {
	d, _ := time.ParseDuration(s)
	return d
}

// runTab is runOnce's tail for a Solo tab Lane.
func runTab(env *Env, t *Task, h harness.Harness, command *harness.Command, launch harness.Launch, rec *Record, reportPath string, allChecks []string, b *brief.Brief) *Record {
	idle, _ := time.ParseDuration(t.IdleLimit)
	wall, _ := time.ParseDuration(t.WallLimit)
	ui.Eventf("dispatch", "task %s  %s %s %s  %s  tab  wall %s  → %s", t.Task, h.Name(), t.Route.Model, t.Route.Effort, t.Access, t.WallLimit, relOrAbs(t.Project, t.Folder))
	out, timer, err := env.Solo.TabLane(env, t, h, command, launch.Pointer, rec, idle, wall)
	if err != nil {
		rec.Outcome.FailureClass = FailLane
		rec.Outcome.Outcome = OutcomeFailed
		rec.Outcome.ErrorText = cut(err.Error(), 500)
		finish(rec)
		ui.Eventf("failed", "task %s  %s: %s", t.Task, FailLane, firstLine(err.Error()))
		return rec
	}
	switch timer {
	case "cancelled":
		rec.Outcome.Outcome = OutcomeCancelled
		finish(rec)
		return rec
	case "idle", "wall":
		rec.Outcome.Timer = timer
		rec.Outcome.FailureClass = FailStall
		rec.Outcome.Outcome = OutcomeTimedOut
		finish(rec)
		ui.Eventf("stall", "task %s  %s limit passed in the tab", t.Task, timer)
		return rec
	}
	rec.Outcome.ModelRan = t.Route.Model
	report, found := rec.Report()
	if !found {
		rec.Outcome.FailureClass = FailTransient
		rec.Outcome.Outcome = OutcomeFailed
		rec.Outcome.ErrorText = "the tab ended with no report"
		finish(rec)
		return rec
	}
	_ = out
	return judge(env, t, rec, report, allChecks, b)
}
