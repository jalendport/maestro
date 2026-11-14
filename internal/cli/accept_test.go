package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/run"
)

func TestResumeAcceptQueuesOneAnswer(t *testing.T) {
	path := os.Getenv("PATH")
	rec, s := waitFixture(t)
	t.Setenv("PATH", path)
	out, err := exec.Command("git", "-C", s.Project, "init", "-q").CombinedOutput()
	if err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(s.Project, "change.txt"), []byte("accepted work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rec.Path, dispatch.FileReport), []byte("## Summary\nKeep the change.\n\nSTATUS: done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec.State, rec.Outcome.Outcome = "ended", dispatch.OutcomeFailed
	rec.Verification.Changed = []string{"change.txt"}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	s.Tasks[0].State = run.TaskBlocked
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	output, err := waitCommand(t, "resume", s.Run, "--task", "1", "--accept", "--json")
	var queued struct{ Resume run.Resume }
	if err != nil || json.Unmarshal([]byte(output), &queued) != nil || !queued.Resume.Accept || queued.Resume.Task != rec.Task || !run.PendingResume(s.Folder, rec.Task) {
		t.Fatalf("accept answer: %s, %v", output, err)
	}
}

func TestResumeAcceptRejectsOtherAnswers(t *testing.T) {
	for _, other := range [][]string{{"--lane", "codex-sol"}, {"--brief", "corrected.md"}, {"--skip"}, {"--merge"}, {"--finish"}} {
		t.Run(other[0], func(t *testing.T) {
			_, s := waitFixture(t)
			args := append([]string{"resume", s.Run, "--task", "1", "--accept"}, other...)
			if _, err := waitCommand(t, args...); err == nil || !strings.Contains(err.Error(), "exactly one") {
				t.Fatalf("combined accept with %s: %v", other[0], err)
			}
		})
	}
}

func TestWaitAndStatusShowAcceptedTask(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[asJSON], func(t *testing.T) {
			rec, s := waitFixture(t)
			rec.State = "ended"
			rec.Outcome = dispatch.Outcome{Outcome: dispatch.OutcomeFailed, Status: "done", FailureClass: dispatch.FailBad, Decision: "accepted"}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			s.Tasks[0].State, s.Tasks[0].Decision = run.TaskDone, "accepted"
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			for _, verb := range []string{"wait", "status"} {
				args := []string{verb, rec.Task}
				if asJSON {
					args = append(args, "--json")
				}
				if output, err := waitCommand(t, args...); err != nil || !strings.Contains(output, "accepted") {
					t.Fatalf("%s accepted Task: %s, %v", verb, output, err)
				}
			}
		})
	}
}

func TestStatisticsKeepAcceptanceSeparateFromGatePass(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	for _, row := range []*dispatch.Record{
		{Kind: "dispatch", Attempt: 1, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, Decision: "blocked"}},
		{Kind: "acceptance", Attempt: 1, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeFailed, Decision: "accepted"}},
	} {
		if err := ledger.Append(row); err != nil {
			t.Fatal(err)
		}
	}
	root := (&app{}).build()
	root.SetArgs([]string{"router", "stats", "--json"})
	output := captureStdout(t, root.Execute)
	var stats routerStats
	if err := json.Unmarshal([]byte(output), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Routes != 1 || stats.Attempted[""] != 1 || stats.Verified[""] != 0 {
		t.Fatalf("acceptance counted as a Route or gate pass: %+v", stats)
	}
}
