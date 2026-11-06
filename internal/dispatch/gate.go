package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/jev"
)

const (
	CauseBrief       = "brief"
	CauseEnvironment = "environment"
	CauseCapability  = "capability"
	CauseNone        = "none"
)

// Gate is the Jev verification gate's answer.
type Gate struct {
	Satisfies         float64         `json:"satisfies,omitempty"`
	Verdict           string          `json:"verdict,omitempty"`
	VerdictConfidence float64         `json:"verdict_confidence,omitempty"`
	Risk              string          `json:"risk,omitempty"`
	RiskConfidence    float64         `json:"risk_confidence,omitempty"`
	Model             string          `json:"jev_model,omitempty"`
	ElapsedMS         int64           `json:"elapsed_ms,omitempty"`
	DiffBytes         int             `json:"diff_bytes,omitempty"`
	Skipped           string          `json:"skipped,omitempty"`
	Cause             string          `json:"cause,omitempty"`
	CauseSource       string          `json:"cause_source,omitempty"`
	DoneItems         map[int]float64 `json:"done_items,omitempty"`
}

// maxDiffBytes keeps the gate request under the Jev state limit (about 15k tokens).
const maxDiffBytes = 60 * 1024

// runGate asks Jev whether the diff satisfies the Brief. It runs only after
// the deterministic checks passed; it never closes a Task on its own.
func runGate(env *Env, t *Task, rec *Record, b *brief.Brief, report *brief.Report, snap attemptBaseline) *Gate {
	return askGate(env, t, rec, b, report, snap, true)
}

func askGate(env *Env, t *Task, rec *Record, b *brief.Brief, report *brief.Report, snap attemptBaseline, full bool) *Gate {
	fallback := func(reason string) *Gate {
		return &Gate{Skipped: reason, Cause: ruleCause(rec), CauseSource: "rule"}
	}
	if env.Roster.Router.Backend == "rules" || (env.Project != nil && env.Project.Router == "rules") {
		return fallback("router = rules")
	}
	client, err := jev.New(env.Roster.Router.BaseURL, env.Roster.Router.Model, 5*time.Second)
	if err != nil {
		return fallback(err.Error())
	}
	diff, err := snap.diff(t.workdir(), rec.Verification.Changed)
	if err != nil && full {
		return fallback(err.Error())
	}
	if full && strings.TrimSpace(diff) == "" {
		return fallback("empty diff")
	}
	blocked := strings.Join(rec.Permission.Blocked, "\n")
	status := rec.Outcome.Status
	if report != nil {
		blocked = report.Sections["Blocked"]
		status = report.Status
	}
	var failedChecks strings.Builder
	for _, check := range rec.Verification.Checks {
		if !check.OK && !check.Skipped {
			fmt.Fprintf(&failedChecks, "%s\n%s\n%s\n", cut(check.Command, maxDiffBytes), cut(check.Reason, maxDiffBytes), cut(check.Output, maxDiffBytes))
			if failedChecks.Len() >= maxDiffBytes {
				break
			}
		}
	}
	state := map[string]any{
		"brief":   map[string]any{"title": cut(t.Title, maxDiffBytes), "goal": cut(b.Goal, maxDiffBytes), "done": cut(b.Done, maxDiffBytes)},
		"blocked": cut(blocked, maxDiffBytes), "status": cut(status, maxDiffBytes),
		"failed_checks": cut(failedChecks.String(), maxDiffBytes), "error": cut(rec.Outcome.ErrorText, maxDiffBytes),
		"diff": cut(diff, maxDiffBytes),
	}
	if env.Project != nil {
		client.Redact = env.Project.Redact
	}
	questions := map[string]jev.Question{
		"cause": jev.Choice("Why did the Task go wrong?", map[string]any{
			CauseBrief:       "a required file is outside touch, Done asks for a forbidden change, or the Brief contradicts itself",
			CauseEnvironment: "a tool or service was denied, down, or missing, such as a sandbox denial, hung Docker, or missing module",
			CauseCapability:  "nothing was in the way; the Lane's work is wrong or incomplete",
			CauseNone:        "nothing went wrong",
		}),
	}
	if full {
		for i, item := range doneItems(b.Done) {
			questions[fmt.Sprintf("done_%d", i+1)] = jev.Noul(fmt.Sprintf("Does the diff satisfy this Done item? Item %d: %s", i+1, cut(item, maxDiffBytes)))
		}
		for name, question := range map[string]jev.Question{
			"satisfies": jev.Noul("Does the diff satisfy every item under Done in the brief?"),
			"verdict": jev.Choice("Classify the diff against the brief.", map[string]any{
				"pass":            "the diff does what the brief asks and nothing else",
				"wrong_logic":     "the diff attempts the brief but its logic is wrong",
				"incomplete":      "the diff does part of the brief",
				"behavior_change": "the diff changes behavior the brief did not ask for",
				"scope_creep":     "the diff touches files or features outside the brief",
			}),
			"risk": jev.Score("How risky is this diff to merge as it stands?", []string{"none", "low", "medium", "high", "critical"}),
		} {
			questions[name] = question
		}
	}
	resp, err := client.Ask(context.Background(), state, questions)
	if err != nil {
		g := fallback(err.Error())
		g.DiffBytes = len(diff)
		return g
	}
	risk := resp.Answers["risk"]
	g := &Gate{
		Satisfies: resp.Answers["satisfies"].Noul,
		Verdict:   resp.Answers["verdict"].Choice, VerdictConfidence: resp.Answers["verdict"].Confidence,
		Risk: risk.Legend[fmt.Sprint(int(risk.Score+0.5))], RiskConfidence: risk.Confidence,
		Model: resp.Model, ElapsedMS: resp.Elapsed.Milliseconds(), DiffBytes: len(diff),
		Cause: resp.Answers["cause"].Choice, CauseSource: "jev",
	}
	if !validCause(g.Cause) {
		g.Cause, g.CauseSource = ruleCause(rec), "rule"
	}
	if full {
		g.DoneItems = map[int]float64{}
		for i := range doneItems(b.Done) {
			if answer, ok := resp.Answers[fmt.Sprintf("done_%d", i+1)]; ok {
				g.DoneItems[i+1] = answer.Noul
			}
		}
	}
	return g
}

func validCause(cause string) bool {
	return cause == CauseBrief || cause == CauseEnvironment || cause == CauseCapability || cause == CauseNone
}

func ruleCause(rec *Record) string {
	switch {
	case len(rec.Verification.TouchViolations) > 0 || rec.Outcome.Status == brief.StatusBlocked:
		return CauseBrief
	case rec.Outcome.Status == brief.StatusPartial && len(rec.Permission.Blocked) > 0:
		return CauseEnvironment
	}
	for _, check := range rec.Verification.Checks {
		if !check.OK && !check.Skipped && check.Reason == "Docker does not respond" {
			return CauseEnvironment
		}
	}
	return CauseCapability
}

func ensureCause(env *Env, t *Task, rec *Record, b *brief.Brief, report *brief.Report, snap attemptBaseline) {
	if rec.Outcome.FailureClass != FailBad {
		return
	}
	if rec.Outcome.Decision == "" {
		rec.Outcome.Decision = "failed"
		if rec.Outcome.Outcome == OutcomeBlocked {
			rec.Outcome.Decision = "blocked"
		}
	}
	if len(rec.Verification.TouchViolations) > 0 {
		rec.Verification.Gate = (&Gate{Cause: CauseBrief, CauseSource: "rule"}).raw()
		return
	}
	var gate Gate
	_ = json.Unmarshal(rec.Verification.Gate, &gate)
	if validCause(gate.Cause) {
		return
	}
	rec.Verification.Gate = askGate(env, t, rec, b, report, snap, false).raw()
}

// ResolveCause keeps old Records usable when they have no Cause yet.
func (rec *Record) ResolveCause() string {
	var gate Gate
	_ = json.Unmarshal(rec.Verification.Gate, &gate)
	if !validCause(gate.Cause) {
		gate.Cause, gate.CauseSource = ruleCause(rec), "rule"
		rec.Verification.Gate = gate.raw()
	}
	return gate.Cause
}

// BlockedReason gives the Orchestrator the Cause and the Lane's Blocked items.
func (rec *Record) BlockedReason() string {
	text := rec.Outcome.ErrorText
	if len(rec.Permission.Blocked) > 0 {
		text = strings.Join(rec.Permission.Blocked, "; ")
	}
	var gate Gate
	if json.Unmarshal(rec.Verification.Gate, &gate) == nil && gate.Cause != "" {
		return "cause " + gate.Cause + ": " + text
	}
	return text
}

// collectDiff is the diff of the paths the Lane changed: tracked ones against
// the start commit, new ones in full. Other uncommitted work in the tree is
// not the Lane's and stays out. Cut at maxDiffBytes.
func collectDiff(root, start string, changed []string) string {
	var w strings.Builder
	if start != "" && len(changed) > 0 {
		for _, rev := range [][]string{{start + "..HEAD"}, {"HEAD"}} {
			args := append([]string{"-C", root, "-c", "core.fsmonitor=false", "-c", "diff.external=", "diff", "--no-ext-diff", "--no-textconv"}, rev...)
			args = append(args, "--")
			args = append(args, changed...)
			if b, err := exec.Command("git", args...).Output(); err == nil {
				w.Write(b)
			}
		}
	}
	for _, p := range changed {
		if w.Len() > maxDiffBytes {
			break
		}
		if start != "" {
			if err := exec.Command("git", "-C", root, "cat-file", "-e", "HEAD:"+p).Run(); err == nil {
				continue // tracked now: covered by git diff
			}
		}
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil || isBinary(b) {
			continue
		}
		fmt.Fprintf(&w, "\n--- new file: %s\n%s\n", p, string(b))
	}
	s := w.String()
	if len(s) > maxDiffBytes {
		s = s[:maxDiffBytes] + "\n[diff cut]\n"
	}
	return s
}

func isBinary(b []byte) bool {
	for _, c := range b[:min(len(b), 512)] {
		if c == 0 {
			return true
		}
	}
	return false
}

func (g *Gate) raw() json.RawMessage {
	b, _ := json.Marshal(g)
	return b
}

// DiffFor returns the diff of a finished Dispatch's changed paths.
func DiffFor(root string, rec *Record) string {
	if rec.Verification.CommitRange != "" {
		args := []string{"-C", root, "diff", rec.Verification.CommitRange}
		if !strings.Contains(rec.Verification.CommitRange, "..") {
			args = []string{"-C", root, "show", "--format=", rec.Verification.CommitRange}
		}
		args = append(args, "--")
		args = append(args, rec.Verification.Changed...)
		if out, err := exec.Command("git", args...).Output(); err == nil {
			return string(out)
		}
	}
	if rec.Workdir != "" {
		root = rec.Workdir
	}
	if b, err := os.ReadFile(filepath.Join(rec.Path, FileBaseline)); err == nil {
		var snap attemptBaseline
		if json.Unmarshal(b, &snap) == nil && snap.Contents != nil {
			diff, err := snap.diff(root, rec.Verification.Changed)
			if err == nil {
				return diff
			}
			return ""
		}
	}
	return collectDiff(root, rec.Verification.StartCommit, rec.Verification.Changed)
}
