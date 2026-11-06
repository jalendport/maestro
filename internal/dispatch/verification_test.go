package dispatch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/ui"
)

func TestCommittedAttemptReachesScopeGuardAndGateDiff(t *testing.T) {
	testGitEnv(t)
	root, folder := t.TempDir(), t.TempDir()
	git := func(args ...string) {
		t.Helper()
		args = append([]string{"-C", root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	git("init")
	path := filepath.Join(root, "work.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "Initial")
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("committed attempt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-am", "Attempt")
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "rules"}}}
	task := &Task{Project: root, Folder: folder, Access: brief.AccessEdit, Commit: true, Touch: []string{"allowed.txt"}}
	rec := &Record{Path: folder, Verification: Verification{StartCommit: snap.Commit}}
	judge(env, task, rec, brief.ParseReport("STATUS: done\n"), nil, &brief.Brief{}, snap)
	if !reflect.DeepEqual(rec.Verification.Changed, []string{"work.txt"}) || rec.Outcome.ErrorText != "scope: work.txt" || rec.Verification.CommitRange == "" {
		t.Fatalf("record: %+v", rec)
	}
	diff := DiffFor(root, rec)
	if !strings.Contains(diff, "-before") || !strings.Contains(diff, "+committed attempt") {
		t.Fatalf("committed diff missing: %s", diff)
	}
}

func TestCheckFailureNamesCommandAndFirstOutputLine(t *testing.T) {
	for _, command := range []string{"printf 'compile failed\nmore detail\n'; false", "printf 'test failed\nmore detail\n'; false"} {
		t.Run(command, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root, folder := t.TempDir(), t.TempDir()
			env := &Env{Roster: &roster.Roster{}}
			rec := &Record{Path: folder}
			task := &Task{Project: root, Access: brief.AccessEdit}
			judge(env, task, rec, brief.ParseReport("STATUS: done\n"), []string{command}, &brief.Brief{}, attemptBaseline{})
			want := "check failed: " + command + ": " + ui.FirstLine(rec.Verification.Checks[0].Output, 0)
			if rec.Outcome.ErrorText != want || rec.Outcome.Outcome != OutcomeFailed {
				t.Fatalf("outcome: %+v, want %q", rec.Outcome, want)
			}
		})
	}
}

func TestCheckStopReasonAndClassReachLedger(t *testing.T) {
	for _, tc := range []struct {
		name, command, idle, wall, reason, class string
	}{
		{"idle", "printf started; sleep 30", "100ms", "1m", "no output for 100ms", FailStall},
		{"wall", "while true; do printf x; sleep 0.05; done", "1s", "150ms", "wall limit", FailStall},
		{"docker", "docker compose test", "1s", "1m", "Docker does not respond", FailBad},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root, folder := causeRoot(t), t.TempDir()
			if tc.name == "docker" {
				bin := t.TempDir()
				if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "rules"}}}
			// The Lane has used more than the checks' entire Wall limit.
			rec := &Record{Kind: "dispatch", Path: folder, StartedAt: time.Now().Add(-time.Hour)}
			task := &Task{Project: root, Access: brief.AccessEdit, IdleLimit: tc.idle, WallLimit: tc.wall}
			commands := []string{"false", tc.command, "touch must-not-run"}
			judge(env, task, rec, brief.ParseReport("STATUS: done\n"), commands, &brief.Brief{}, attemptBaseline{})
			want := "check failed: " + tc.command + ": " + tc.reason
			if rec.Outcome.Outcome != OutcomeFailed || rec.Outcome.FailureClass != tc.class || rec.Outcome.ErrorText != want {
				t.Fatalf("outcome: %+v", rec.Outcome)
			}
			if tc.name == "docker" {
				if cause := rec.ResolveCause(); cause != CauseEnvironment {
					t.Fatalf("Docker Cause: %s", cause)
				}
			}
			checks := rec.Verification.Checks
			if len(checks) != 3 || checks[0].Skipped || checks[0].Reason != "" || checks[1].Skipped || !checks[2].Skipped || checks[2].Reason != tc.reason {
				t.Fatalf("checks: %+v", checks)
			}
			if err := ledger.Append(rec); err != nil {
				t.Fatal(err)
			}
			rows := 0
			if err := ledger.Each(func(raw json.RawMessage) error {
				rows++
				var stored Record
				if err := json.Unmarshal(raw, &stored); err != nil {
					return err
				}
				if stored.Outcome.FailureClass != tc.class || stored.Outcome.ErrorText != want || !reflect.DeepEqual(stored.Verification.Checks, checks) {
					t.Fatalf("Ledger row: %+v", stored)
				}
				return nil
			}); err != nil || rows != 1 {
				t.Fatalf("Ledger rows=%d: %v", rows, err)
			}
			stored, err := LoadRecord(folder)
			if err != nil || stored.Outcome.FailureClass != tc.class || stored.Outcome.ErrorText != want {
				t.Fatalf("saved record: %+v, %v", stored, err)
			}
			if tc.name == "docker" {
				assertCauseOnlyGate(t, rec, CauseEnvironment, "rule")
			}
		})
	}
}

func TestRerunWallLimitKeepsBadResultAndSkippedChecksInLedger(t *testing.T) {
	root, folder := causeRoot(t), t.TempDir()
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "rules"}}}
	task := &Task{Project: root, Access: brief.AccessEdit, IdleLimit: "1m", WallLimit: "1s"}
	rec := &Record{Kind: "dispatch", Path: folder, StartedAt: time.Now()}
	command := "printf 'call\\n' >> calls; if [ ! -f first ]; then touch first; printf 'first failure\\nmore detail'; exit 3; fi; printf 'rerun output'; sleep 30"
	commands := []string{command, "touch must-not-run", "docker compose test"}
	judge(env, task, rec, brief.ParseReport("STATUS: done\n"), commands, &brief.Brief{}, attemptBaseline{})
	want := "check failed: " + command + ": first failure"
	if rec.Outcome.Outcome != OutcomeFailed || rec.Outcome.FailureClass != FailBad || rec.Outcome.ErrorText != want {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	checks := rec.Verification.Checks
	if len(checks) != 3 || checks[0].Command != command || checks[0].OK || checks[0].Skipped || checks[0].FailureClass != FailBad || checks[0].Output != "first failure\nmore detail" || checks[0].Reason != "" {
		t.Fatalf("checks: %+v", checks)
	}
	for i, check := range checks[1:] {
		if check.Command != commands[i+1] || !check.Skipped || check.OK || check.Reason != "wall limit after a rerun" || check.FailureClass != FailBad || check.Output != "" || check.Duration != "0s" {
			t.Fatalf("remaining check: %+v", check)
		}
		found := false
		for _, event := range rec.Events {
			found = found || (event.Kind == "check" && strings.Contains(event.Text, check.Command+": skipped: wall limit after a rerun"))
		}
		if !found {
			t.Fatalf("skipped check absent from events: %+v", rec.Events)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "must-not-run")); !os.IsNotExist(err) {
		t.Fatalf("skipped check ran: %v", err)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil || string(calls) != "call\ncall\n" {
		t.Fatalf("calls: %q, %v", calls, err)
	}
	if err := ledger.Append(rec); err != nil {
		t.Fatal(err)
	}
	rows := 0
	if err := ledger.Each(func(raw json.RawMessage) error {
		rows++
		var stored Record
		if err := json.Unmarshal(raw, &stored); err != nil {
			return err
		}
		if stored.Outcome != rec.Outcome || !reflect.DeepEqual(stored.Verification.Checks, checks) {
			t.Fatalf("Ledger row: %+v", stored)
		}
		return nil
	}); err != nil || rows != 1 {
		t.Fatalf("Ledger rows=%d: %v", rows, err)
	}
	stored, err := LoadRecord(folder)
	if err != nil || stored.Outcome != rec.Outcome || !reflect.DeepEqual(stored.Verification.Checks, checks) {
		t.Fatalf("saved record: %+v, %v", stored, err)
	}
}

func TestPartialBlockedReportKeepsBadResultAfterCheckStall(t *testing.T) {
	for _, limit := range []string{"idle", "wall"} {
		for _, blocked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/blocked=%t", limit, blocked), func(t *testing.T) {
				root, folder := causeRoot(t), t.TempDir()
				env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "rules"}}}
				task := &Task{Project: root, Access: brief.AccessEdit, IdleLimit: "50ms", WallLimit: "1m"}
				if limit == "wall" {
					task.IdleLimit, task.WallLimit = "1m", "50ms"
				}
				text := "## Blocked\n- none\nSTATUS: partial"
				if blocked {
					text = "## Blocked\n- tool is missing\nSTATUS: partial"
				}
				rec := &Record{Path: folder}
				judge(env, task, rec, brief.ParseReport(text), []string{"sleep 30", "touch must-not-run"}, &brief.Brief{}, attemptBaseline{})
				checks := rec.Verification.Checks
				if len(checks) != 2 || checks[0].FailureClass != FailStall || !checks[1].Skipped {
					t.Fatalf("checks: %+v", checks)
				}
				want := FailStall
				if blocked {
					want = FailBad
					if rec.Outcome.ErrorText != "" {
						t.Fatalf("Stall replaced the Report error text: %s", rec.Outcome.ErrorText)
					}
					if cause := rec.ResolveCause(); cause != CauseEnvironment {
						t.Fatalf("partial Report Cause: %s", cause)
					}
					assertCauseOnlyGate(t, rec, CauseEnvironment, "rule")
				}
				if rec.Outcome.Outcome != OutcomeFailed || rec.Outcome.FailureClass != want {
					t.Fatalf("outcome: %+v", rec.Outcome)
				}
				stored, err := LoadRecord(folder)
				if err != nil || stored.Outcome.FailureClass != want || (blocked && stored.ResolveCause() != CauseEnvironment) {
					t.Fatalf("saved record: %+v error=%v", stored, err)
				}
			})
		}
	}
}

func TestPartialBlockedReportGetsDockerPreflightError(t *testing.T) {
	root, folder := causeRoot(t), t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "rules"}}}
	task := &Task{Project: root, Access: brief.AccessEdit, IdleLimit: "1s", WallLimit: "1m"}
	rec := &Record{Kind: "dispatch", Path: folder}
	judge(env, task, rec, brief.ParseReport("## Blocked\n- tool is missing\nSTATUS: partial"), []string{"docker compose test", "touch must-not-run"}, &brief.Brief{}, attemptBaseline{})
	want := "check failed: docker compose test: Docker does not respond"
	if rec.Outcome.Outcome != OutcomeFailed || rec.Outcome.FailureClass != FailBad || rec.Outcome.ErrorText != want || rec.ResolveCause() != CauseEnvironment {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	checks := rec.Verification.Checks
	if len(checks) != 2 || checks[0].FailureClass != FailBad || checks[0].Reason != "Docker does not respond" || !checks[1].Skipped {
		t.Fatalf("checks: %+v", checks)
	}
	stored, err := LoadRecord(folder)
	if err != nil || stored.Outcome.ErrorText != want || stored.ResolveCause() != CauseEnvironment {
		t.Fatalf("saved record: %+v error=%v", stored, err)
	}
	assertCauseOnlyGate(t, rec, CauseEnvironment, "rule")
}

func TestNoChecksReasonAppearsInPrintedStatus(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"answers":{"satisfies":{"noul":1},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}}}}`)
	}))
	defer server.Close()
	root, folder := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}}
	task := &Task{Project: root, Access: brief.AccessEdit}
	rec := &Record{Path: folder, Route: &router.Route{}}
	judge(env, task, rec, brief.ParseReport("STATUS: done\n"), nil, &brief.Brief{}, snap)
	if rec.Outcome.Outcome != OutcomeUnverified || rec.Outcome.ErrorText != "no checks configured" {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	if !strings.Contains(Status(rec), "unverified (no checks configured)") {
		t.Fatalf("status: %s", Status(rec))
	}
	file, err := os.CreateTemp(t.TempDir(), "print")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	old := os.Stdout
	func() { os.Stdout = file; defer func() { os.Stdout = old }(); Print(rec, false) }()
	printed, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ui.FirstLine(string(printed), 0), "no checks configured") {
		t.Fatalf("print: %s", printed)
	}
}

func TestRunOnceDrainsFinalStreamBeforeWait(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	payload := filepath.Join(t.TempDir(), "stream")
	final, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "result": "## Answer\nfinal line\nSTATUS: done"})
	stream := strings.Repeat("{\"type\":\"ignored\"}\n", 100000) + string(final)
	if err := os.WriteFile(payload, []byte(stream), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_STREAM", payload)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nexec cat \"$MAESTRO_TEST_STREAM\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"claude": bin})}
	task := &Task{Project: root, Folder: folder, Task: "abcd", Access: brief.AccessRead, Route: &router.Route{Harness: "claude"}}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.Outcome != OutcomeAnswered {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	for _, event := range rec.Events {
		if event.Kind == "stream" {
			t.Fatalf("stream error: %+v", event)
		}
	}
	output, err := os.ReadFile(filepath.Join(folder, FileOutput))
	if err != nil || string(output) != stream+"\n" {
		t.Fatalf("incomplete stream: bytes=%d, want=%d, err=%v", len(output), len(stream)+1, err)
	}
}
