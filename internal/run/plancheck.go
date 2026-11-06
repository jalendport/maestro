package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

const planCheckQuestions = `Read and search only. Check the Plan against the project tree for these defects:
1. A path named in a Task's Files, touch, or text that does not exist and that the Task does not say it creates.
2. A function, type, flag, or config key a Task names as existing that a search does not find.
3. A Task that removes or renames an exported symbol while a caller of it is outside the Task's touch.
4. A path a Task's text says the Task changes (or a doc the Plan's shared rules say must agree with the code) that is outside the Task's touch.
5. Two Tasks with no blocked_by between them whose text changes the same file while their Files lists do not show the overlap.
6. A Done item that the Task's touch or the shared rules make impossible.

In the Findings of your Report, write one line per defect in this exact form:
task <n>: <defect>; fix: <what to change in the Plan>
Write nothing else in Findings. If there is no defect, write the single line none.
This Findings rule replaces the example Findings line in the Report template.`

func planCheckBrief(planPath, root string) *brief.Brief {
	return &brief.Brief{
		Fields: brief.Fields{Title: "Plan check", Category: "review", Access: brief.AccessRead, Level: "some", Ceiling: "daily"},
		Goal:   fmt.Sprintf("Plan file: `%s`\nProject root: `%s`\n\n%s", planPath, root, planCheckQuestions),
		Done:   "The Report answers the six questions against the tree and uses the required Findings line form.",
		Files:  []string{planPath},
	}
}

// RenderPlanCheck renders the read Brief, including the standard Report shape.
func RenderPlanCheck(planPath, root, reportPath string) string {
	return brief.Render(planCheckBrief(planPath, root), brief.RenderOpts{Access: brief.AccessRead, ReportPath: reportPath})
}

func needsPlanCheck(env *dispatch.Env, plan *brief.Plan, o CreateOpts) bool {
	if o.NoPlanCheck || (env.Roster.Run.PlanCheck != nil && !*env.Roster.Run.PlanCheck) {
		return false
	}
	edits := 0
	for _, b := range plan.Tasks {
		if b.EffectiveAccess() == brief.AccessEdit {
			edits++
		}
	}
	return edits >= 2
}

func preparePlanCheck(env *dispatch.Env, s *State) error {
	folder := filepath.Join(s.Folder, "plan-check")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return err
	}
	b := planCheckBrief(s.Plan, s.Project)
	b.Path = filepath.Join(folder, "brief.source.md")
	if err := brief.Write(b, b.Path); err != nil {
		return err
	}
	t, err := dispatch.PrepareIn(env, b, router.Request{Ceiling: roster.TierDaily}, s.Run, "plan-check", s.Folder)
	if err != nil {
		return err
	}
	// A Plan check has one attempt and a fixed Wall limit, below the Tier limit.
	t.WallLimit, t.MaxAttempt = "5m", 1
	if err := t.Save(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(folder, dispatch.FileBrief), []byte(RenderPlanCheck(s.Plan, s.Project, filepath.Join(folder, dispatch.FileReport))), 0o644); err != nil {
		return err
	}
	s.State, s.PlanCheck = StateHeld, "running"
	s.Approval = append(s.Approval, "plan check running")
	return s.Save()
}

// detachPlanCheck starts the Run supervisor, which first collects the check.
var detachPlanCheck = func(folder string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(folder, "supervisor.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(self, "_run", folder)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func executePlanCheck(env *dispatch.Env, s *State) error {
	if err := withControlLock(s.Folder, func() error {
		current, err := Load(s.Folder)
		if err != nil {
			return err
		}
		if current.PlanCheck != "running" || SupervisorAlive(current) {
			return fmt.Errorf("Run %s already has a supervisor or a Plan check result", s.Run)
		}
		*s = *current
		s.Pid = os.Getpid()
		return s.Save()
	}); err != nil {
		return err
	}
	t, err := dispatch.LoadTask(filepath.Join(s.Folder, "plan-check"))
	var rec *dispatch.Record
	if err == nil {
		rec, err = dispatch.Execute(env, t)
	}
	finishPlanCheck(s, rec, err)
	s.Pid = 0
	return s.Save()
}

var planFinding = regexp.MustCompile(`^task [1-9][0-9]*: .+; fix: .+$`)

func planCheckFindings(rec *dispatch.Record, err error) ([]string, string) {
	if err != nil {
		return nil, err.Error()
	}
	if rec == nil {
		return nil, "missing Dispatch"
	}
	if rec.Outcome.Outcome != dispatch.OutcomeAnswered {
		reason := rec.Outcome.ErrorText
		if reason == "" {
			reason = rec.Outcome.Outcome
		}
		return nil, reason
	}
	r, ok := rec.Report()
	if !ok || r.Status != brief.StatusDone || strings.Join(r.Order, ",") != "Answer,Findings,Concerns" {
		return nil, "unshaped Report"
	}
	findings := strings.TrimSpace(r.Sections["Findings"])
	if findings == "none" {
		return nil, ""
	}
	lines := strings.Split(findings, "\n")
	for _, line := range lines {
		if !planFinding.MatchString(line) {
			return nil, "unshaped Findings"
		}
	}
	return lines, ""
}

func finishPlanCheck(s *State, rec *dispatch.Record, err error) {
	approval := make([]string, 0, len(s.Approval))
	for _, line := range s.Approval {
		if line != "plan check running" {
			approval = append(approval, line)
		}
	}
	findings, reason := planCheckFindings(rec, err)
	for _, finding := range findings {
		approval = append(approval, "plan check: "+finding)
	}
	s.PlanCheck = "done"
	if reason != "" {
		s.PlanCheckNote = "plan check: no result (" + strings.Join(strings.Fields(reason), " ") + ")"
	}
	s.Approval = approval
	if len(approval) == 0 {
		s.State = StateLive
	}
}
