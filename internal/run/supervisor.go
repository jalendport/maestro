package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/solo"
	"github.com/jalendport/maestro/internal/ui"
)

// CreateOpts are the run verb's flags.
type CreateOpts struct {
	Lane    string
	Level   roster.Level
	Floor   roster.Tier
	Ceiling roster.Tier
}

// Create parses the Plan file, lays out the Run folder with one Brief file
// per Task, routes every Task provisionally, and decides whether the
// breakdown needs approval. It starts a detached Council for a held Plan,
// but does not start any Task.
func Create(env *dispatch.Env, planPath string, o CreateOpts) (*State, error) {
	if idx, live := LiveRunFor(env.Root); live {
		return nil, fmt.Errorf("Run %s is live; use maestro add", idx.Run)
	}
	plan, err := brief.ParsePlan(planPath)
	if err != nil {
		return nil, err
	}
	if err := config.EnsureIgnoredDir(config.RunsDir(env.Root)); err != nil {
		return nil, err
	}
	runID, err := dispatch.NextRunID(env.Root)
	if err != nil {
		return nil, err
	}
	folder := filepath.Join(config.RunsDir(env.Root), runID)
	if err := os.MkdirAll(filepath.Join(folder, "control"), 0o755); err != nil {
		return nil, err
	}
	s := &State{Run: runID, Project: env.Root, Plan: plan.Path, Title: plan.Title, State: StateHeld, StartedAt: time.Now(), Folder: folder, Lane: o.Lane, Level: o.Level, Floor: o.Floor, Ceiling: o.Ceiling}
	rt := env.NewRouter()
	var approval []string
	for i, b := range plan.Tasks {
		id := dispatch.NewTaskID(folder)
		taskDir := filepath.Join(folder, id)
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			return nil, err
		}
		bp := filepath.Join(taskDir, "brief.source.md")
		if err := brief.Write(b, bp); err != nil {
			return nil, err
		}
		ts := &TaskState{Number: i + 1, ID: id, Title: b.Title, BriefPath: bp, Access: b.EffectiveAccess(), Category: b.Category, BlockedBy: b.BlockedBy, State: TaskWaiting, Provisional: true, Folder: taskDir}
		if ts.BlockedBy == nil {
			ts.BlockedBy = []string{}
		}
		req := router.Request{Brief: b, Access: ts.Access, Root: env.Root, Lane: firstNonEmpty(b.Lane, o.Lane), Level: o.Level, Project: env.Project, InSolo: solo.InSolo()}
		req.Floor, req.Ceiling = routeBounds(s, b)
		route, err := rt.Route(req)
		switch e := err.(type) {
		case nil:
			ts.Route = route
			if route.Band == "fallback" || route.Band == "medium" {
				approval = append(approval, fmt.Sprintf("task %d routed with %s confidence", i+1, route.Band))
			}
			if route.GapFlag {
				approval = append(approval, fmt.Sprintf("task %d: level estimate %s is two levels from the router's %s", i+1, route.LevelEstimate, route.Level))
			}
		case *router.AskOrchestrator:
			ts.Waiting = e.Reason
			approval = append(approval, fmt.Sprintf("task %d: %s", i+1, e.Reason))
		case *router.CouncilSignal:
			ts.Waiting = e.Reason
			approval = append(approval, fmt.Sprintf("task %d wants a council: %s", i+1, e.Reason))
		default:
			return nil, fmt.Errorf("task %d: %w", i+1, err)
		}
		s.Tasks = append(s.Tasks, ts)
	}
	if len(plan.Tasks) > 4 {
		approval = append(approval, fmt.Sprintf("%d tasks (more than 4)", len(plan.Tasks)))
	}
	s.Approval = approval
	if len(approval) == 0 {
		s.State = StateLive
	}
	if err := s.Save(); err != nil {
		return nil, err
	}
	if len(approval) > 0 {
		// Plan Council: Heavy, over the Plan file, so the
		// Orchestrator approves the breakdown with the Merge in hand.
		rel, _ := filepath.Rel(env.Root, plan.Path)
		c, err := council.Create(env, council.Opts{
			Question: "Review this plan before it runs: are the tasks complete, correctly ordered, and bounded? Name any task whose Goal or Done is too thin to start.",
			Files:    []string{rel}, Tier: roster.TierHeavy, Trigger: council.TriggerPlan, Run: runID, Slug: runID + "-plan",
		})
		if err != nil {
			ui.Warnf("plan council: %v", err)
		} else {
			s.Councils = append(s.Councils, c.Slug)
			_ = s.Save()
			if err := council.Detach(c.Path); err != nil {
				ui.Warnf("plan council: %v", err)
				return s, nil
			}
			ui.Eventf("council", "plan council %s started (%d seats); maestro wait %s", c.Slug, len(c.Seats), c.Slug)
		}
	}
	return s, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Supervisor drives one live Run.
type Supervisor struct {
	Env   *dispatch.Env
	State *State
	mu    sync.Mutex
	// running holds the cancel channel of every running Task.
	running map[string]chan struct{}
	results chan result
	// escalation tracking for the Run pause
	escalationsInARow int
	errorTexts        map[string]int
	paused            bool
	cancelled         bool
}

type result struct {
	task *TaskState
	rec  *dispatch.Record
	err  error
}

// Start loads run.json and runs the loop until the Run finishes or pauses.
func Start(env *dispatch.Env, folder string) error {
	s, err := Load(folder)
	if err != nil {
		return err
	}
	if s.State == StateHeld {
		if _, err := os.Stat(filepath.Join(folder, "control", "cancel")); err != nil {
			return fmt.Errorf("Run %s is held for approval; run maestro approve %s", s.Run, s.Run)
		}
	}
	if s.State == StateFinished || s.State == StateCancelled {
		return fmt.Errorf("Run %s is %s", s.Run, s.State)
	}
	sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{}, results: make(chan result, 16), errorTexts: map[string]int{}}
	return sup.loop()
}

func (sup *Supervisor) loop() error {
	s := sup.State
	s.State = StateLive
	s.Pid = os.Getpid()
	s.PauseReason = ""
	s.EndedAt = time.Time{}
	if err := WriteIndex(s); err != nil {
		return err
	}
	defer RemoveIndex(s.Project, s.Run)
	stopAwake := proc.KeepAwake(sup.Env.Roster.KeepAwake())
	s.Awake = stopAwake != nil
	defer func() {
		if stopAwake != nil {
			stopAwake()
		}
	}()
	sup.consumeControl()
	_ = s.Save()
	ui.Eventf("run", "%s  %s  %d tasks  live", s.Run, s.Title, len(s.Tasks))

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		sup.consumeControl()
		sup.updateDependencies()
		if !sup.paused && !sup.cancelled {
			sup.schedule()
		}
		if len(sup.running) == 0 {
			if sup.cancelled {
				return sup.end(StateCancelled)
			}
			if sup.paused {
				return sup.end(StatePaused)
			}
			if !sup.anyStartable() {
				return sup.end(sup.completionState())
			}
		}
		select {
		case r := <-sup.results:
			sup.handle(r)
		case <-ticker.C:
			sup.touch()
		}
	}
}

// schedule starts every Task that is ready and has a slot.
func (sup *Supervisor) schedule() {
	s := sup.State
	caps := sup.Env.Roster.Concurrency
	editRunning := false
	for id := range sup.running {
		if t, ok := s.Task(id); ok && t.Access == brief.AccessEdit {
			editRunning = true
		}
	}
	// Ready read Tasks first, then edit Tasks, each in plan order.
	for _, access := range []string{brief.AccessRead, brief.AccessEdit} {
		for _, t := range s.Tasks {
			sup.consumeControl()
			if sup.cancelled || sup.paused {
				return
			}
			if t.State != TaskWaiting || t.Access != access {
				continue
			}
			if why := sup.blockedReason(t); why != "" {
				t.Waiting = why
				continue
			}
			if len(sup.running) >= caps.MaxParallel {
				t.Waiting = "run cap"
				continue
			}
			if total := sup.machineRunning(); total >= caps.MaxTotal {
				t.Waiting = "machine cap"
				continue
			}
			if access == brief.AccessEdit && editRunning {
				t.Waiting = "tree busy"
				continue
			}
			if acct := sup.accountFor(t); acct != "" && sup.accountRunning(acct) >= caps.MaxPerAccount {
				t.Waiting = "account cap " + acct
				continue
			}
			sup.start(t)
			if access == brief.AccessEdit {
				editRunning = true
			}
		}
	}
	_ = s.Save()
}

// dependencyReason follows the graph, including waiting Tasks, to find a
// terminal blocker. The boolean distinguishes a blocker from work still live.
func (sup *Supervisor) dependencyReason(t *TaskState, visiting map[*TaskState]bool) (string, bool) {
	if visiting[t] {
		return fmt.Sprintf("dependency cycle at task %d", t.Number), true
	}
	visiting[t] = true
	defer delete(visiting, t)
	waiting := ""
	for _, ref := range t.BlockedBy {
		b, ok := sup.State.Task(ref)
		if !ok {
			return fmt.Sprintf("missing dependency %q", ref), true
		}
		if b.State == TaskDone {
			continue
		}
		if b.DependencyBlocked || b.State == TaskWaiting || b.State == TaskRunning {
			if why, terminal := sup.dependencyReason(b, visiting); terminal {
				return why, true
			}
		} else {
			switch b.State {
			case TaskFailed, TaskBlocked, TaskSkipped, TaskCancelled:
				return fmt.Sprintf("blocked by %d (%s)", b.Number, b.State), true
			}
		}
		if waiting == "" {
			waiting = fmt.Sprintf("blocked by %d", b.Number)
		}
	}
	return waiting, false
}

func (sup *Supervisor) blockedReason(t *TaskState) string {
	why, _ := sup.dependencyReason(t, map[*TaskState]bool{})
	return why
}

// updateDependencies also releases dependency blocks after an upstream resume.
func (sup *Supervisor) updateDependencies() {
	for _, t := range sup.State.Tasks {
		if t.State != TaskWaiting && !t.DependencyBlocked {
			continue
		}
		why, terminal := sup.dependencyReason(t, map[*TaskState]bool{})
		if terminal {
			t.State, t.Error, t.Waiting = TaskBlocked, why, ""
			t.DependencyBlocked = true
		} else if t.DependencyBlocked {
			t.State, t.Error, t.Waiting = TaskWaiting, "", why
			t.DependencyBlocked = false
		}
	}
}

// anyStartable reports whether every dependency of a waiting Task is done.
func (sup *Supervisor) anyStartable() bool {
	for _, t := range sup.State.Tasks {
		if t.State == TaskWaiting && sup.blockedReason(t) == "" {
			return true
		}
	}
	return false
}

func (sup *Supervisor) completionState() string {
	for _, t := range sup.State.Tasks {
		if t.State != TaskDone && t.State != TaskSkipped {
			return StatePaused
		}
	}
	return StateFinished
}

// Run flags override the per-Task Brief, as they do for a Dispatch.
func routeBounds(s *State, b *brief.Brief) (roster.Tier, roster.Tier) {
	floor, ceiling := s.Floor, s.Ceiling
	if floor == "" {
		floor, _ = roster.ParseTier(b.Floor)
	}
	if ceiling == "" {
		ceiling, _ = roster.ParseTier(b.Ceiling)
	}
	return floor, ceiling
}

func (sup *Supervisor) accountFor(t *TaskState) string {
	if t.Route != nil {
		return t.Route.Account
	}
	return ""
}

func (sup *Supervisor) accountRunning(acct string) int {
	n := 0
	for id := range sup.running {
		if t, ok := sup.State.Task(id); ok && t.Route != nil && t.Route.Account == acct {
			n++
		}
	}
	for _, idx := range LiveRuns() {
		if idx.Project == sup.State.Project && idx.Run == sup.State.Run {
			continue
		}
		if other, err := Load(idx.Folder); err == nil {
			for _, t := range other.Tasks {
				if t.State == TaskRunning && t.Route != nil && t.Route.Account == acct {
					n++
				}
			}
		}
	}
	return n
}

func (sup *Supervisor) machineRunning() int {
	n := len(sup.running)
	for _, idx := range LiveRuns() {
		if idx.Project == sup.State.Project && idx.Run == sup.State.Run {
			continue
		}
		if other, err := Load(idx.Folder); err == nil {
			_, _, _, running, _, _ := other.Counts()
			n += running
		}
	}
	return n
}

// start routes the Task at Dispatch time and runs it in a goroutine.
func (sup *Supervisor) start(t *TaskState) {
	b, err := brief.Parse(t.BriefPath)
	if err != nil {
		t.State = TaskFailed
		t.Error = err.Error()
		return
	}
	req := router.Request{Lane: firstNonEmpty3(t.ResumeLane, b.Lane, sup.State.Lane), Level: sup.State.Level, Excluded: t.Excluded}
	req.Floor, req.Ceiling = routeBounds(sup.State, b)
	if t.Escalations > 0 && t.Route != nil && req.Lane == "" {
		// Escalation: the new pick must be a higher Tier.
		if next := nextTier(t.Route.Tier); next != "" && (req.Floor == "" || next.Rank() > req.Floor.Rank()) {
			req.Floor = next
		}
	}
	task, err := dispatch.Prepare(sup.Env, b, req, sup.State.Run, t.ID)
	sup.consumeControl()
	if sup.cancelled || t.State != TaskWaiting {
		return
	}
	if err != nil {
		sup.escalate(t, nil, err.Error())
		return
	}
	task.FirstAttempt = t.Attempt + 1
	task.LedgerByRun = true
	task.MaxAttempt = min(t.Attempt+2, sup.Env.Roster.Failure.MaxDispatches)
	cancel := make(chan struct{})
	task.Cancel = cancel
	sup.running[t.ID] = cancel
	t.State = TaskRunning
	t.Waiting = ""
	t.Route = task.Route
	t.Provisional = false
	t.StartedAt = time.Now()
	t.LastOutput = time.Now()
	t.ResumeLane = ""
	go func() {
		rec, err := dispatch.Execute(sup.Env, task)
		sup.results <- result{task: t, rec: rec, err: err}
	}()
}

func nextTier(t roster.Tier) roster.Tier {
	switch t {
	case roster.TierLight:
		return roster.TierDaily
	case roster.TierDaily:
		return roster.TierHeavy
	case roster.TierHeavy:
		return roster.TierFrontier
	}
	return ""
}

func firstNonEmpty3(a, b, c string) string { return firstNonEmpty(a, firstNonEmpty(b, c)) }

// handle applies one finished Dispatch to the Task and the Run.
func (sup *Supervisor) handle(r result) {
	t := r.task
	if r.rec != nil {
		defer func() {
			if r.rec.Path != "" {
				if err := r.rec.Save(); err != nil {
					ui.Warnf("dispatch record: %v", err)
				}
			}
			if err := ledger.Append(r.rec); err != nil {
				ui.Warnf("ledger: %v", err)
			}
		}()
	}
	delete(sup.running, t.ID)
	t.EndedAt = time.Now()
	if t.Cancel {
		t.State = TaskCancelled
		t.Outcome = dispatch.OutcomeCancelled
		if r.rec != nil {
			t.Attempt = r.rec.Attempt
		}
		_ = sup.State.Save()
		return
	}
	if r.err != nil {
		sup.escalate(t, nil, r.err.Error())
		_ = sup.State.Save()
		return
	}
	rec := r.rec
	repeated := repeatedError(t, rec)
	t.Attempt = rec.Attempt
	t.Outcome = rec.Outcome.Outcome
	t.Status = rec.Outcome.Status
	t.Error = rec.Outcome.ErrorText
	switch rec.Outcome.Outcome {
	case dispatch.OutcomeVerified, dispatch.OutcomeAnswered, dispatch.OutcomeUnverified:
		t.State = TaskDone
		sup.escalationsInARow = 0
	case dispatch.OutcomeBlocked:
		t.State = TaskBlocked
		sup.noteEscalation(t, "blocked")
	case dispatch.OutcomeCancelled:
		t.State = TaskCancelled
	default:
		if repeated != "" {
			t.State = TaskBlocked
			t.Waiting = ""
			t.Error = "same error twice: " + repeated
			sup.pause(t.Error)
		} else {
			sup.escalate(t, rec, rec.Outcome.ErrorText)
		}
	}
	_ = sup.State.Save()
}

func repeatedError(t *TaskState, rec *dispatch.Record) string {
	if !failedOutcome(rec.Outcome.Outcome) || rec.Attempt < 2 {
		return ""
	}
	previous := ""
	if t.Attempt == rec.Attempt-1 && failedOutcome(t.Outcome) {
		previous = t.Error
	}
	// Dispatch can retry on the same Lane before the supervisor gets a result.
	if t.Folder != "" {
		folder := filepath.Join(t.Folder, dispatch.FileAttempts, fmt.Sprint(rec.Attempt-1))
		if last, err := dispatch.LoadRecord(folder); err == nil && last.Attempt == rec.Attempt-1 {
			previous = ""
			if failedOutcome(last.Outcome.Outcome) {
				previous = last.Outcome.ErrorText
			}
		}
	}
	text := failureText(rec.Outcome.ErrorText)
	if text != "" && text == failureText(previous) {
		return text
	}
	return ""
}

func failedOutcome(outcome string) bool {
	switch outcome {
	case dispatch.OutcomeFailed, dispatch.OutcomeEscalated, dispatch.OutcomeTimedOut:
		return true
	}
	return false
}

func failureText(text string) string {
	for _, class := range []string{dispatch.FailLane, dispatch.FailTransient, dispatch.FailLimit, dispatch.FailStall, dispatch.FailBad} {
		if rest, ok := strings.CutPrefix(text, class+":"); ok {
			return strings.TrimPrefix(rest, " ")
		}
	}
	return text
}

// escalate removes the failed family and re-Routes one Tier up, until the
// Dispatch cap or the ceiling sends the Task to the Orchestrator.
func (sup *Supervisor) escalate(t *TaskState, rec *dispatch.Record, errText string) {
	if t.Route != nil {
		t.Excluded = appendUnique(t.Excluded, t.Route.FamilyID)
	}
	t.Escalations++
	if rec != nil {
		t.Attempt = rec.Attempt
	}
	if t.Attempt >= sup.Env.Roster.Failure.MaxDispatches || (t.Route != nil && t.Route.Tier == roster.TierFrontier) || rec == nil {
		t.State = TaskFailed
		t.Error = errText
		sup.noteEscalation(t, errText)
		return
	}
	t.State = TaskWaiting
	rec.Outcome.Outcome = dispatch.OutcomeEscalated
	t.Outcome = dispatch.OutcomeEscalated
	t.Waiting = "re-route after " + ui.FirstLine(errText, 80)
	ui.Eventf("escalate", "task %s  %s; re-routing above %s", t.ID, ui.FirstLine(errText, 80), t.Route.Tier)
}

// noteEscalation counts Orchestrator hand-offs for the Run pause rule.
func (sup *Supervisor) noteEscalation(t *TaskState, errText string) {
	sup.escalationsInARow++
	key := ui.FirstLine(errText, 80)
	if key != "" {
		sup.errorTexts[key]++
	}
	f := sup.Env.Roster.Failure
	switch {
	case f.RunPauseAfterEscalations > 0 && sup.escalationsInARow >= f.RunPauseAfterEscalations:
		sup.pause(fmt.Sprintf("%d escalations in a row (task %d)", sup.escalationsInARow, t.Number))
	case f.RunPauseAfterSameError > 0 && key != "" && sup.errorTexts[key] >= f.RunPauseAfterSameError:
		sup.pause(fmt.Sprintf("%d tasks with the same error (task %d): %s", sup.errorTexts[key], t.Number, key))
	}
}

func (sup *Supervisor) pause(reason string) {
	if sup.paused {
		return
	}
	sup.paused = true
	sup.State.PauseReason = reason
	ui.Eventf("pause", "%s: %s; running dispatches finish, no new dispatch starts", sup.State.Run, reason)
}

// touch refreshes last-output times from the running records for status.
func (sup *Supervisor) touch() {
	for id := range sup.running {
		t, ok := sup.State.Task(id)
		if !ok {
			continue
		}
		if st, err := os.Stat(filepath.Join(t.Folder, dispatch.FileOutput)); err == nil {
			t.LastOutput = st.ModTime()
		}
	}
	_ = sup.State.Save()
}

// consumeControl applies marker files written by add, cancel, and resume.
func (sup *Supervisor) consumeControl() {
	ctl := filepath.Join(sup.State.Folder, "control")
	if path := filepath.Join(ctl, "cancel"); fileExists(path) {
		sup.cancelled = true
		sup.State.PauseReason = ""
		for _, t := range sup.State.Tasks {
			sup.cancelTask(t)
		}
		_ = sup.State.Save()
		_ = os.Remove(path)
	}
	if sup.cancelled {
		return
	}
	entries, _ := os.ReadDir(ctl)
	for _, e := range entries {
		path := filepath.Join(ctl, e.Name())
		name := e.Name()
		switch {
		case strings.HasPrefix(name, "add-") && strings.HasSuffix(name, ".md"):
			sup.addTask(path)
		case strings.HasPrefix(name, "cancel-"):
			ref := strings.TrimPrefix(name, "cancel-")
			if t, ok := sup.State.Task(ref); ok {
				sup.cancelTask(t)
			}
		case strings.HasPrefix(name, "resume-"):
			sup.applyResume(path)
		default:
			continue
		}
		_ = os.Remove(path)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (sup *Supervisor) cancelTask(t *TaskState) {
	if ch, running := sup.running[t.ID]; running {
		select {
		case <-ch:
		default:
			close(ch)
		}
		t.Cancel = true
	} else if t.State == TaskWaiting {
		t.State = TaskCancelled
		t.Outcome = dispatch.OutcomeCancelled
		t.Waiting = ""
		t.EndedAt = time.Now()
	}
}

func (sup *Supervisor) addTask(path string) {
	b, err := brief.Parse(path)
	if err != nil {
		ui.Warnf("add: %v", err)
		return
	}
	id := dispatch.NewTaskID(sup.State.Folder)
	taskDir := filepath.Join(sup.State.Folder, id)
	_ = os.MkdirAll(taskDir, 0o755)
	bp := filepath.Join(taskDir, "brief.source.md")
	if err := brief.Write(b, bp); err != nil {
		ui.Warnf("add: %v", err)
		return
	}
	ts := &TaskState{Number: len(sup.State.Tasks) + 1, ID: id, Title: b.Title, BriefPath: bp, Access: b.EffectiveAccess(), Category: b.Category, BlockedBy: b.BlockedBy, State: TaskWaiting, Folder: taskDir}
	if ts.BlockedBy == nil {
		ts.BlockedBy = []string{}
	}
	sup.State.Tasks = append(sup.State.Tasks, ts)
	ui.Eventf("add", "task %d  %s", ts.Number, ts.Title)
}

// Resume is the answer file maestro resume writes.
type Resume struct {
	Task  string `json:"task"`
	Lane  string `json:"lane,omitempty"`
	Brief string `json:"brief,omitempty"`
	Skip  bool   `json:"skip,omitempty"`
}

func (sup *Supervisor) applyResume(path string) {
	var r Resume
	b, err := os.ReadFile(path)
	if err != nil || jsonUnmarshal(b, &r) != nil {
		return
	}
	t, ok := sup.State.Task(r.Task)
	if !ok {
		ui.Warnf("resume: no task %q", r.Task)
		return
	}
	switch {
	case r.Skip:
		t.State = TaskSkipped
		t.Waiting = ""
	case r.Brief != "":
		corrected, err := brief.Parse(r.Brief)
		if err != nil {
			ui.Warnf("resume: %v", err)
			return
		}
		// Dispatch re-reads the full Brief, including touch, checks, and wall limit.
		// Refresh the fields that the scheduler uses before it can claim a slot.
		t.Access = corrected.EffectiveAccess()
		t.Category = corrected.Category
		t.Cancel = false
		t.BriefPath = r.Brief
		t.State = TaskWaiting
		t.Waiting = "resumed with a corrected brief"
		t.Excluded = nil
	case r.Lane != "":
		t.Cancel = false
		t.ResumeLane = r.Lane
		t.State = TaskWaiting
		t.Waiting = "resumed on " + r.Lane
		t.Excluded = nil
	}
	t.DependencyBlocked = false
	sup.updateDependencies()
	sup.escalationsInARow = 0
	sup.errorTexts = map[string]int{}
	sup.paused = false
	sup.State.PauseReason = ""
	ui.Eventf("resume", "task %d  %s", t.Number, t.Waiting)
}

// end writes the final state, the report, the ledger row, and tells the user.
func (sup *Supervisor) end(state string) error {
	s := sup.State
	s.State = state
	s.EndedAt = time.Now()
	s.Pid = 0
	sup.updateDependencies()
	if state == StatePaused {
		var needs []string
		for _, t := range sortedTasks(s) {
			if t.State == TaskFailed || t.State == TaskBlocked || t.State == TaskCancelled || t.State == TaskWaiting {
				needs = append(needs, fmt.Sprintf("task %d (%s)", t.Number, t.State))
			}
		}
		if len(needs) > 0 {
			if s.PauseReason != "" {
				s.PauseReason += "; "
			}
			s.PauseReason += "needs an answer: " + strings.Join(needs, ", ")
		}
	}
	var review *council.Council
	if state == StateFinished {
		review = sup.endOfRunCouncil()
	}
	report := Report(s)
	if err := os.WriteFile(filepath.Join(s.Folder, FileReport), []byte(report), 0o644); err != nil {
		return err
	}
	if err := ledger.Append(ledgerRow(s)); err != nil {
		ui.Warnf("ledger: %v", err)
	}
	done, blocked, failed, _, _, _ := s.Counts()
	summary := fmt.Sprintf("Run %s %s: %d done, %d blocked, %d failed", s.Run, state, done, blocked, failed)
	if review != nil {
		summary += "; council " + review.Slug + " (maestro wait " + review.Slug + ")"
	}
	ui.Eventf(state, "%s  → %s", summary, filepath.Join(s.Folder, FileReport))
	notify(summary)
	if sup.Env.Solo != nil {
		title := s.Title
		if review != nil {
			title += "; council " + review.Slug
		}
		sup.Env.Solo.Wake("run end", s.Run, title, "", "")
	}
	if err := s.Save(); err != nil {
		return err
	}
	RemoveIndex(s.Project, s.Run)
	if review != nil {
		if err := council.Detach(review.Path); err != nil {
			ui.Warnf("end-of-run council: %v", err)
		}
	}
	return nil
}

// endOfRunCouncil prepares a Council when the Run had a Heavy or Frontier Task, when the
// gate scored a high risk, or always when the project asks for it.
func (sup *Supervisor) endOfRunCouncil() *council.Council {
	s := sup.State
	var top roster.Tier
	trigger := false
	var authors []string
	var changed []string
	seen := map[string]bool{}
	for _, t := range s.Tasks {
		if t.Route == nil || t.State != TaskDone {
			continue
		}
		if top == "" || t.Route.Tier.Rank() > top.Rank() {
			top = t.Route.Tier
		}
		if t.Route.Tier.Rank() >= roster.TierHeavy.Rank() {
			trigger = true
		}
		authors = appendUnique(authors, t.Route.FamilyID)
		if rec, err := dispatch.LoadRecord(t.Folder); err == nil {
			var g struct {
				Risk string `json:"risk"`
			}
			if len(rec.Verification.Gate) > 0 && jsonUnmarshal(rec.Verification.Gate, &g) == nil && (g.Risk == "high" || g.Risk == "critical") {
				trigger = true
			}
			for _, p := range rec.Verification.Changed {
				if !seen[p] {
					seen[p] = true
					changed = append(changed, p)
				}
			}
		}
	}
	if sup.Env.Project != nil && sup.Env.Project.Councils == "always" {
		trigger = true
	}
	s.CouncilSkip = ""
	switch {
	case len(changed) == 0:
		s.CouncilSkip = "no changed files"
	case top == "":
		s.CouncilSkip = "no completed Route"
	case !trigger:
		s.CouncilSkip = "no heavy task, risk under high"
	}
	if s.CouncilSkip != "" {
		return nil
	}
	c, err := council.Create(sup.Env, council.Opts{
		Question: fmt.Sprintf("Review the changes of run %s (%s) as one diff: is the work correct, complete, and safe to keep? The files changed are listed under Files; read them in full.", s.Run, s.Title),
		Files:    changed, Tier: top, Trigger: council.TriggerEndOfRun, Run: s.Run, Authors: authors, Slug: s.Run + "-review",
	})
	if err != nil {
		ui.Warnf("end-of-run council: %v", err)
		s.CouncilSkip = "could not create: " + err.Error()
		return nil
	}
	s.Councils = append(s.Councils, c.Slug)
	ui.Eventf("council", "end-of-run council %s  %s  %d seats", c.Slug, c.Tier, len(c.Seats))
	return c
}

func ledgerRow(s *State) map[string]any {
	tasks := make([]map[string]any, 0, len(s.Tasks))
	for _, t := range s.Tasks {
		fam := ""
		if t.Route != nil {
			fam = t.Route.FamilyID
		}
		tasks = append(tasks, map[string]any{"number": t.Number, "id": t.ID, "title": t.Title, "state": t.State, "outcome": t.Outcome, "attempts": t.Attempt, "family": fam})
	}
	return map[string]any{
		"kind": "run", "project": s.Project, "run": s.Run, "plan_path": s.Plan, "tasks": tasks, "state": s.State,
		"pause_reason": s.PauseReason, "councils": s.Councils, "started_at": s.StartedAt, "ended_at": s.EndedAt,
		"duration_s": s.EndedAt.Sub(s.StartedAt).Seconds(),
	}
}

// notify posts a macOS notification; silent when osascript is absent.
func notify(text string) {
	if _, err := exec.LookPath("osascript"); err != nil {
		return
	}
	_ = exec.Command("osascript", "-e", fmt.Sprintf(`display notification %q with title "Maestro"`, text)).Run()
}

func appendUnique(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

// sortedTasks returns Tasks in plan order.
func sortedTasks(s *State) []*TaskState {
	out := append([]*TaskState(nil), s.Tasks...)
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}
