package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func TestRenderPlanCheck(t *testing.T) {
	text := RenderPlanCheck("/project/plan.md", "/project", "/run/plan-check/result.md")
	for _, want := range []string{"1. A path", "2. A function", "3. A Task", "4. A path", "5. Two Tasks", "6. A Done item", "/project/plan.md", "Project root: `/project`", "task <n>: <defect>; fix: <what to change in the Plan>", "single line none", brief.ReportShape(brief.AccessRead, "/run/plan-check/result.md", false)} {
		if !strings.Contains(text, want) {
			t.Errorf("Brief has no %q", want)
		}
	}
}

func planCheckEnv(t *testing.T, findings string, timeout bool) (*dispatch.Env, string, *int) {
	t.Helper()
	sup := worktreeSupervisor(t)
	bin := filepath.Join(t.TempDir(), "read-lane")
	report := "## Answer\nChecked the Plan.\n\n## Findings\n" + findings + "\n\n## Concerns\nnone\n\nSTATUS: done\n"
	message, err := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": report}})
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nread pointer\nprintf '%s\\n' '" + strings.ReplaceAll(string(message), "'", "'\"'\"'") + "'\n"
	if timeout {
		script = "#!/bin/sh\nread pointer\nexec sleep 10\n"
	} else if findings == "fail" {
		script = "#!/bin/sh\nread pointer\nexit 1\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sup.Env.Reg = harness.New(map[string]string{"codex": bin})
	sup.Env.Disc = &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{
		"codex": {Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}},
	}}
	path := filepath.Join(sup.Env.Root, "plan.md")
	writeCheckPlan(t, path, 2)
	started := new(int)
	oldDetach := detachPlanCheck
	detachPlanCheck = func(string) error { *started++; return nil }
	t.Cleanup(func() { detachPlanCheck = oldDetach })
	return sup.Env, path, started
}

func writeCheckPlan(t *testing.T, path string, count int) {
	t.Helper()
	text := "---\ntitle: Check fixture\naccess: edit\ncategory: implement\nlane: codex-sol:medium\nlevel: some\ntouch: ['*.txt']\n---\n"
	for i := range count {
		text += fmt.Sprintf("\n## Task %d\n### Goal\nCreate the fixture file.\n### Done\nThe file exists.\n", i+1)
	}
	runWrite(t, path, text)
}

func TestPlanCheckFakeLane(t *testing.T) {
	for _, tc := range []struct {
		name, findings string
		timeout, held  bool
	}{
		{name: "none", findings: "none"},
		{name: "finding", findings: "task 2: missing path; fix: correct the path", held: true},
		{name: "timeout", timeout: true},
		{name: "unshaped", findings: "- bad finding"},
		{name: "failed", findings: "fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, plan, starts := planCheckEnv(t, tc.findings, tc.timeout)
			s, err := Create(env, plan, CreateOpts{NoCouncil: true})
			if err != nil {
				t.Fatal(err)
			}
			if s.State != StateHeld || s.PlanCheck != "running" || strings.Join(s.Approval, ",") != "plan check running" || *starts != 1 {
				t.Fatalf("check did not hold: %+v, starts %d", s, *starts)
			}
			if err := Approve(env, s); err == nil {
				t.Fatal("approved while check runs")
			}
			task, err := dispatch.LoadTask(filepath.Join(s.Folder, "plan-check"))
			if err != nil {
				t.Fatal(err)
			}
			if task.Access != brief.AccessRead || task.Category != "review" || task.Route.Tier.Rank() > roster.TierDaily.Rank() || task.WallLimit != "5m" || task.MaxAttempt != 1 {
				t.Fatalf("check Dispatch: %+v", task)
			}
			if tc.timeout {
				task.WallLimit = "20ms"
				if err := task.Save(); err != nil {
					t.Fatal(err)
				}
			}
			if err := executePlanCheck(env, s); err != nil {
				t.Fatal(err)
			}
			if (s.State == StateHeld) != tc.held || s.PlanCheck != "done" {
				t.Fatalf("result: %+v", s)
			}
			if tc.held {
				if strings.Join(s.Approval, ",") != "plan check: "+tc.findings {
					t.Fatalf("findings: %v", s.Approval)
				}
				writeCheckPlan(t, plan, 1)
				if err := Approve(env, s); err != nil {
					t.Fatal(err)
				}
				if s.State != StateLive || *starts != 1 || len(s.Tasks) != 1 {
					t.Fatalf("approval did not reread Plan: %+v, starts %d", s, *starts)
				}
			}
			if (tc.timeout || tc.name == "unshaped" || tc.name == "failed") && !strings.Contains(Status(s), "plan check: no result (") {
				t.Fatalf("no failure note: %s", Status(s))
			}
		})
	}
}

func TestPlanCheckSkip(t *testing.T) {
	for _, mode := range []string{"flag", "config", "one edit", "read only"} {
		t.Run(mode, func(t *testing.T) {
			env, path, starts := planCheckEnv(t, "none", false)
			opts := CreateOpts{NoCouncil: true}
			switch mode {
			case "flag":
				opts.NoPlanCheck = true
			case "config":
				d, _, _ := config.ParseFile(roster.Defaults)
				u, _, err := config.ParseFile("[run]\nplan_check = false\n")
				if err != nil {
					t.Fatal(err)
				}
				env.Roster.Run = roster.Merge(d, u).Run
			case "one edit":
				writeCheckPlan(t, path, 1)
			case "read only":
				text, _ := os.ReadFile(path)
				runWrite(t, path, strings.ReplaceAll(string(text), "access: edit", "access: read"))
			}
			s, err := Create(env, path, opts)
			if err != nil {
				t.Fatal(err)
			}
			if *starts != 0 || s.PlanCheck != "" {
				t.Fatalf("started skipped check: %+v", s)
			}
		})
	}
}
