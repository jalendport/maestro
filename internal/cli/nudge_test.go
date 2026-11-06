package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/jev"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

type nudgeCase struct {
	name, verdict, risk, secondVerdict, secondStatus, check string
	itemScore, secondScore                                  float64
	attempts, laneError                                     int
	want                                                    string
	noScore, noChecks                                       bool
}

func TestGateNudgeAndWaitReport(t *testing.T) {
	// The Dispatch part of the cases runs in parallel: each case has its own
	// project, Jev server, and Lane script, and the environment below is the same
	// for every case. The wait part changes the working directory, so it runs one
	// case at a time after all Dispatches end.
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "")
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	cases := []nudgeCase{
		{name: "verified", attempts: 2, secondScore: 0.80, want: dispatch.OutcomeVerified},
		{name: "no second nudge", attempts: 2, secondScore: 0.55, want: dispatch.OutcomeUnverified},
		{name: "wrong logic", verdict: "wrong_logic", attempts: 1, want: dispatch.OutcomeFailed},
		{name: "incomplete", verdict: "incomplete", attempts: 1, want: dispatch.OutcomeFailed},
		{name: "high risk", risk: "high", attempts: 1, want: dispatch.OutcomeUnverified},
		{name: "critical risk", risk: "critical", attempts: 1, want: dispatch.OutcomeUnverified},
		{name: "unknown risk", risk: "unknown", attempts: 1, want: dispatch.OutcomeUnverified},
		{name: "no missing item", itemScore: 0.5, attempts: 1, want: dispatch.OutcomeUnverified},
		{name: "no item answer", noScore: true, attempts: 1, want: dispatch.OutcomeUnverified},
		{name: "no checks", noChecks: true, attempts: 1, want: dispatch.OutcomeUnverified},
		{name: "failed checks", check: "false", attempts: 1, want: dispatch.OutcomeFailed},
		{name: "nudge fails gate", secondVerdict: "wrong_logic", attempts: 2, secondScore: 0.80, want: dispatch.OutcomeFailed},
		{name: "nudge partial", secondStatus: "partial", attempts: 2, want: dispatch.OutcomeFailed},
		{name: "nudge blocked", secondStatus: "blocked", attempts: 2, want: dispatch.OutcomeBlocked},
		{name: "Lane error before nudge", laneError: 1, attempts: 3, secondScore: 0.80, want: dispatch.OutcomeVerified},
		{name: "Lane error during nudge", laneError: 2, attempts: 3, secondScore: 0.80, want: dispatch.OutcomeVerified},
	}
	// The parent owns the projects, so they stay for the wait part.
	roots := make([]string, len(cases))
	dispatched := make([]bool, len(cases))
	for i := range cases {
		roots[i] = t.TempDir()
	}
	t.Run("dispatch", func(t *testing.T) {
		for i, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				dispatchNudgeCase(t, roots[i], tc)
				dispatched[i] = true
			})
		}
	})
	t.Setenv("PATH", "")
	if err := (&discovery.Cache{RefreshedAt: time.Now()}).Save(); err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		t.Run("wait/"+tc.name, func(t *testing.T) {
			if !dispatched[i] {
				t.Fatal("the Dispatch part of this case failed")
			}
			t.Chdir(roots[i])
			output, waitErr := waitCommand(t, "wait", "abcd")
			if !strings.Contains(output, fmt.Sprintf("Attempt %d Report", tc.attempts)) || tc.attempts > 1 && strings.Contains(output, "Attempt 1 Report") {
				t.Fatalf("wait Report=%s error=%v", output, waitErr)
			}
			if tc.want == dispatch.OutcomeVerified && waitErr != nil {
				t.Fatalf("wait: %v", waitErr)
			}
		})
	}
}

// dispatchNudgeCase runs Task abcd of one case in the empty project root.
func dispatchNudgeCase(t *testing.T, root string, tc nudgeCase) {
	t.Helper()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	off := false
	r.Failure.KeepAwake = &off
	verdict, risk := tc.verdict, tc.risk
	if verdict == "" {
		verdict = "pass"
	}
	if risk == "" {
		risk = "medium"
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var request struct {
			Questions map[string]jev.Question
			State     struct{ Diff string }
		}
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		answers := map[string]any{"cause": map[string]string{"choice": dispatch.CauseCapability}}
		if _, full := request.Questions["satisfies"]; full {
			calls++
			for i, text := range []string{"The work file exists.", "The missing behavior works."} {
				question := request.Questions[fmt.Sprintf("done_%d", i+1)]
				if question.Type != "noul" || !strings.Contains(question.Instructions, "Does the diff satisfy this Done item?") || !strings.Contains(question.Instructions, text) {
					t.Errorf("Done question %d: %+v", i+1, question)
				}
			}
			if len(request.Questions) != 6 || request.Questions["cause"].Type != "choice" || request.State.Diff == "" {
				t.Errorf("gate request: %+v", request)
			}
			score, currentVerdict := 0.55, verdict
			if calls > 1 {
				score = tc.secondScore
				if tc.secondVerdict != "" {
					currentVerdict = tc.secondVerdict
				}
			}
			answers["satisfies"] = map[string]float64{"noul": score}
			answers["verdict"] = map[string]string{"choice": currentVerdict}
			answers["risk"] = map[string]any{"score": 2, "legend": map[string]string{"2": risk}}
			answers["done_1"] = map[string]float64{"noul": 0.9}
			if !tc.noScore {
				answers["done_2"] = map[string]float64{"noul": tc.itemScore}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers})
	}))
	defer server.Close()
	r.Router.Backend, r.Router.BaseURL = "jev", server.URL
	bin := filepath.Join(t.TempDir(), "lane")
	// The case values are in the script, not in the shared environment.
	script := fmt.Sprintf(`#!/bin/sh
TEST_NUDGE_COUNT='%s'
TEST_NUDGE_WORK='%s'
TEST_NUDGE_ERROR='%d'
TEST_NUDGE_STATUS='%s'
read pointer
count=0
if [ -f "$TEST_NUDGE_COUNT" ]; then read count < "$TEST_NUDGE_COUNT"; fi
count=$((count + 1))
printf '%%s\n' "$count" > "$TEST_NUDGE_COUNT"
printf 'work %%s\n' "$count" > "$TEST_NUDGE_WORK"
if [ "$count" = "$TEST_NUDGE_ERROR" ]; then printf 'unknown model\n' >&2; exit 1; fi
status=done
if [ "$count" -gt 1 ] && [ -n "$TEST_NUDGE_STATUS" ]; then status=$TEST_NUDGE_STATUS; fi
printf '## Summary\nAttempt %%s Report\n\nSTATUS: %%s\n' "$count" "$status" > "$MAESTRO_TASK_FOLDER/result.md"
`, filepath.Join(t.TempDir(), "count"), filepath.Join(root, "work.txt"), tc.laneError, tc.secondStatus)
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	check := tc.check
	if check == "" {
		check = "true"
	}
	b := &brief.Brief{Fields: brief.Fields{Title: "Complete the work", Access: brief.AccessEdit, Touch: []string{"work.txt"}, Checks: []string{check}}, Goal: "Complete the work.", Done: "- The work file exists.\n- The missing behavior works."}
	if tc.noChecks {
		b.Checks = nil
	}
	b.Path = filepath.Join(root, "source.md")
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	env := &dispatch.Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin}), RetryDelay: time.Millisecond}
	family := r.Families["codex-sol"]
	task, err := dispatch.PrepareRouted(env, b, &router.Route{Family: family, FamilyID: "codex-sol", Harness: "codex", Model: "gpt-6.1-sol", Tier: roster.TierDaily}, "r-0001", "abcd", "")
	if err != nil {
		t.Fatal(err)
	}
	rec, err := dispatch.Execute(env, task)
	if err != nil || rec.Attempt != tc.attempts || rec.Outcome.Outcome != tc.want || rec.Nudged != (tc.attempts > 1) {
		t.Fatalf("Record=%+v error=%v", rec, err)
	}
	// The cases share one Ledger; each case reads only the rows of its project.
	var rows []dispatch.Record
	if err := ledger.Each(func(raw json.RawMessage) error {
		var row dispatch.Record
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row.Project == root {
			rows = append(rows, row)
		}
		return nil
	}); err != nil || len(rows) != tc.attempts {
		t.Fatalf("Ledger=%+v error=%v", rows, err)
	}
	if rows[0].Nudged || rows[len(rows)-1].Nudged != rec.Nudged || rows[len(rows)-1].Route.FamilyID != rows[0].Route.FamilyID {
		t.Fatalf("Ledger nudge or Lane: %+v", rows)
	}
	if rec.Nudged {
		rendered, err := os.ReadFile(filepath.Join(task.Folder, dispatch.FileBrief))
		if err != nil || !strings.Contains(string(rendered), "gate nudge") && tc.laneError != 2 || !strings.Contains(string(rendered), "Done item 2: The missing behavior works.") || strings.Contains(string(rendered), "Done item 1:") {
			t.Fatalf("nudge Brief=%s error=%v", rendered, err)
		}
		saved, err := dispatch.LoadTask(task.Folder)
		if err != nil || !saved.Nudged || saved.MaxAttempt != 3 {
			t.Fatalf("saved Task=%+v error=%v", saved, err)
		}
		first, err := dispatch.LoadRecord(filepath.Join(task.Folder, dispatch.FileAttempts, "1"))
		if err != nil || first.Nudged || first.State != "ended" {
			t.Fatalf("first Record=%+v error=%v", first, err)
		}
		if tc.name == "verified" {
			var gate dispatch.Gate
			if err := json.Unmarshal(first.Verification.Gate, &gate); err != nil || gate.Satisfies != 0.55 || gate.DoneItems[2] != tc.itemScore {
				t.Fatalf("first gate=%+v error=%v", gate, err)
			}
			if err := json.Unmarshal(rec.Verification.Gate, &gate); err != nil || gate.Satisfies != 0.80 {
				t.Fatalf("nudge gate=%+v error=%v", gate, err)
			}
		}
	}
}
