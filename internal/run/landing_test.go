package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/ledger"
)

func TestLandingCheckPausesCombinedFailureAndFixTaskLands(t *testing.T) {
	sup := worktreeSupervisor(t)
	sup.State.Commit = true
	command := "if [ -f first.txt ] && [ -f second.txt ] && [ ! -f fixed.txt ]; then printf 'combined failure\\nmore detail\\n'; exit 1; fi"
	sup.Env.Project = &config.Project{Checks: []string{command}}
	sup.schedule()
	for range 2 {
		rec := takeResult(t, sup)
		if len(rec.Verification.Checks) != 1 || !rec.Verification.Checks[0].OK {
			t.Fatalf("Task checks: %+v", rec.Verification.Checks)
		}
	}
	head := runGit(t, sup.Env.Root, "rev-parse", sup.State.RunBranch)
	if err := sup.end(StateFinished); err != nil {
		t.Fatal(err)
	}
	want := "landing check failed: " + command + ": combined failure"
	if sup.State.State != StatePaused || !sup.State.FinishPending || sup.State.PauseReason != want {
		t.Fatalf("Run did not pause: %+v", sup.State)
	}
	if runGit(t, sup.Env.Root, "rev-parse", sup.State.RunBranch) != head || runGit(t, sup.Env.Root, "rev-list", "--count", "master.."+sup.State.RunBranch) != "2" {
		t.Fatal("landing check lost Task commits")
	}
	for _, text := range []string{Report(sup.State), Status(sup.State), PauseReason(sup.State)} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing failure: %s", text)
		}
	}
	saved, err := Load(sup.State.Folder)
	if err != nil || saved.LandingCheck == nil || saved.LandingCheck.State != "failed" || saved.LandingCheck.Head != head {
		t.Fatalf("saved landing check: %+v, %v", saved, err)
	}
	rows := 0
	if err := ledger.Each(func(raw json.RawMessage) error {
		var row struct {
			Kind         string                 `json:"kind"`
			LandingCheck *dispatch.LandingCheck `json:"landing_check"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row.Kind == "run" {
			rows++
			if row.LandingCheck == nil || row.LandingCheck.State != "failed" {
				t.Fatalf("Ledger lost landing check: %s", raw)
			}
		}
		return nil
	}); err != nil || rows != 1 {
		t.Fatalf("Run Ledger rows: %d, %v", rows, err)
	}
	fix := &brief.Brief{Fields: brief.Fields{Title: "Fix the combined failure", Access: brief.AccessEdit, Touch: []string{"fixed.txt"}}, Goal: "Fix the combined failure.", Done: "The checks pass."}
	path := filepath.Join(sup.State.Folder, "control", "add-fix.md")
	if err := brief.Write(fix, path); err != nil {
		t.Fatal(err)
	}
	if _, err := QueueResume(sup.State.Folder, Resume{Finish: true}); err != nil {
		t.Fatal(err)
	}
	sup.consumeControl()
	task := sup.State.Tasks[2]
	b, err := brief.Parse(task.BriefPath)
	if err != nil {
		t.Fatal(err)
	}
	sup.pending[task] = pendingDispatch{brief: b, route: sup.State.Tasks[0].Route}
	bin := filepath.Join(t.TempDir(), "fix")
	runWrite(t, bin, "#!/bin/sh\nread pointer\nprintf fixed > fixed.txt\nprintf '## Summary\\nFix the combined failure.\\n\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n")
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	sup.Env.Reg = harness.New(map[string]string{"codex": bin})
	sup.schedule()
	if task.State != TaskRunning || runGit(t, task.Workdir, "rev-parse", "HEAD") != head {
		t.Fatalf("fix Task did not start at the merged head: %+v", task)
	}
	takeResult(t, sup)
	if err := sup.end(StateFinished); err != nil || sup.State.State != StateFinished || sup.State.FinishPending {
		t.Fatalf("fixed Run did not land: %+v, %v", sup.State, err)
	}
	if sup.State.LandingCheck.State != "passed" || !strings.Contains(Report(sup.State), "landing check: passed (1 checks, ") {
		t.Fatalf("passed landing check missing: %s", Report(sup.State))
	}
	if got := runGit(t, sup.Env.Root, "rev-list", "--count", saved.LandingCheck.Head+"..HEAD"); got != "1" {
		t.Fatalf("fix commit did not land: %s", got)
	}
}

func TestLandingCheckSkipsAndPasses(t *testing.T) {
	for _, mode := range []string{"no checks", "one Task", "two Tasks", "moved base"} {
		t.Run(mode, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			sup.State.Commit = true
			want := "landing check: passed (1 checks, "
			if mode != "no checks" {
				sup.Env.Project = &config.Project{Checks: []string{"true"}}
			} else {
				want = "landing check: skipped (no project checks)"
			}
			if mode == "one Task" || mode == "moved base" {
				sup.State.Tasks = sup.State.Tasks[:1]
			}
			sup.schedule()
			for range len(sup.State.Tasks) {
				takeResult(t, sup)
			}
			if mode == "one Task" {
				want = "landing check: skipped (one merged Task checked on the same base)"
			}
			if mode == "moved base" {
				runWrite(t, filepath.Join(sup.Env.Root, "user.txt"), "user change\n")
				runGit(t, sup.Env.Root, "add", "user.txt")
				runGit(t, sup.Env.Root, "commit", "-m", "User change")
				sup.Env.Project.Checks = []string{"test -f user.txt"}
			}
			if err := sup.end(StateFinished); err != nil || sup.State.State != StateFinished {
				t.Fatalf("landing failed: %+v, %v", sup.State, err)
			}
			for _, text := range []string{Report(sup.State), Status(sup.State)} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q: %s", want, text)
				}
			}
			if mode == "moved base" && sup.State.LandingCheck.Head != runGit(t, sup.Env.Root, "rev-parse", "HEAD") {
				t.Fatal("landing check did not test the rebased head")
			}
		})
	}
}

func TestLandingCheckStopsAtIdleAndWallLimits(t *testing.T) {
	for _, mode := range []string{"idle", "wall"} {
		t.Run(mode, func(t *testing.T) {
			sup := worktreeSupervisor(t)
			sup.schedule()
			for range 2 {
				takeResult(t, sup)
			}
			sup.Env.Project = &config.Project{Checks: []string{"sleep 30", "touch must-not-run"}}
			sup.Env.Roster.Failure.IdleLimit.Duration = time.Minute
			for _, task := range sup.State.Tasks {
				saved, err := dispatch.LoadTask(task.Folder)
				if err != nil {
					t.Fatal(err)
				}
				saved.WallLimit = "100ms"
				if err := saved.Save(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "idle" {
				sup.Env.Roster.Failure.IdleLimit.Duration = 100 * time.Millisecond
			} else {
				wall := sup.Env.Roster.Failure.WallLimit["daily"]
				wall.Duration = 100 * time.Millisecond
				sup.Env.Roster.Failure.WallLimit["daily"] = wall
			}
			if err := sup.end(StateFinished); err != nil {
				t.Fatal(err)
			}
			checks := sup.State.LandingCheck.Checks
			if sup.State.State != StatePaused || len(checks) != 2 || checks[0].FailureClass != dispatch.FailStall || !checks[1].Skipped {
				t.Fatalf("landing check limits: %+v, %+v", sup.State, checks)
			}
		})
	}
}
