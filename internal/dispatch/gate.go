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

	"github.com/jalendport/maestro/internal/jev"
)

// Gate is the Jev verification gate's answer (jev-router.md section 8).
type Gate struct {
	Satisfies         float64 `json:"satisfies"`
	Verdict           string  `json:"verdict"`
	VerdictConfidence float64 `json:"verdict_confidence"`
	Risk              string  `json:"risk"`
	RiskConfidence    float64 `json:"risk_confidence"`
	Model             string  `json:"jev_model"`
	ElapsedMS         int64   `json:"elapsed_ms"`
	DiffBytes         int     `json:"diff_bytes"`
	Skipped           string  `json:"skipped,omitempty"`
}

// maxDiffBytes keeps the gate request under the Jev state limit (about 15k tokens).
const maxDiffBytes = 60 * 1024

// runGate asks Jev whether the diff satisfies the Brief. It runs only after
// the deterministic checks passed; it never closes a Task on its own.
func runGate(env *Env, t *Task, rec *Record, goal, done string) *Gate {
	if env.Roster.Router.Backend == "rules" || (env.Project != nil && env.Project.Router == "rules") {
		return &Gate{Skipped: "router = rules"}
	}
	client, err := jev.New(env.Roster.Router.BaseURL, env.Roster.Router.Model, 5*time.Second)
	if err != nil {
		return &Gate{Skipped: err.Error()}
	}
	diff := collectDiff(t.Project, rec.Verification.StartCommit, rec.Verification.Changed)
	if strings.TrimSpace(diff) == "" {
		return &Gate{Skipped: "empty diff"}
	}
	state := map[string]any{
		"brief": map[string]any{"title": t.Title, "goal": goal, "done": done},
		"diff":  diff,
	}
	if env.Project != nil {
		for _, r := range env.Project.Redact {
			state["diff"] = strings.ReplaceAll(state["diff"].(string), r, "[redacted]")
		}
	}
	questions := map[string]jev.Question{
		"satisfies": jev.Noul("Does the diff satisfy every item under Done in the brief?"),
		"verdict": jev.Choice("Classify the diff against the brief.", map[string]any{
			"pass":            "the diff does what the brief asks and nothing else",
			"wrong_logic":     "the diff attempts the brief but its logic is wrong",
			"incomplete":      "the diff does part of the brief",
			"behavior_change": "the diff changes behavior the brief did not ask for",
			"scope_creep":     "the diff touches files or features outside the brief",
		}),
		"risk": jev.Score("How risky is this diff to merge as it stands?", []string{"none", "low", "medium", "high", "critical"}),
	}
	resp, err := client.Ask(context.Background(), state, questions)
	if err != nil {
		return &Gate{Skipped: err.Error(), DiffBytes: len(diff)}
	}
	risk := resp.Answers["risk"]
	g := &Gate{
		Satisfies: resp.Answers["satisfies"].Noul,
		Verdict:   resp.Answers["verdict"].Choice, VerdictConfidence: resp.Answers["verdict"].Confidence,
		Risk: risk.Legend[fmt.Sprint(int(risk.Score+0.5))], RiskConfidence: risk.Confidence,
		Model: resp.Model, ElapsedMS: resp.Elapsed.Milliseconds(), DiffBytes: len(diff),
	}
	return g
}

// collectDiff is the diff of the paths the Lane changed: tracked ones against
// the start commit, new ones in full. Other uncommitted work in the tree is
// not the Lane's and stays out. Cut at maxDiffBytes.
func collectDiff(root, start string, changed []string) string {
	var w strings.Builder
	if start != "" && len(changed) > 0 {
		args := append([]string{"-C", root, "diff", start, "--"}, changed...)
		if b, err := exec.Command("git", args...).Output(); err == nil {
			w.Write(b)
		}
	}
	for _, p := range changed {
		if w.Len() > maxDiffBytes {
			break
		}
		if start != "" {
			if err := exec.Command("git", "-C", root, "cat-file", "-e", start+":"+p).Run(); err == nil {
				continue // tracked at start: covered by git diff
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
