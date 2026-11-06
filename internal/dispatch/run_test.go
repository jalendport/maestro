package dispatch

import (
	"encoding/json"
	"fmt"
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
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

func TestRunOnceTakesFreshBaseline(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	folder := t.TempDir()
	old, err := verify.Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SaveTo(filepath.Join(folder, FileBaseline)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "earlier-task.txt"), []byte("earlier work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\ncat >/dev/null\nprintf 'new work\\n' > \"$TEST_CHANGED\"\nprintf 'STATUS: done\\n' > \"$TEST_REPORT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &roster.Roster{}
	r.Router.Backend = "rules"
	env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessEdit,
		Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
	for i, path := range []string{"first.txt", "retry.txt", "escalation.txt"} {
		task.Touch = []string{path}
		t.Setenv("TEST_CHANGED", filepath.Join(root, path))
		rec := runOnce(env, task, &brief.Brief{Title: "Test attempt"}, i+1, nil)
		if rec.Outcome.Outcome != OutcomeUnverified {
			t.Fatalf("attempt %d: outcome %+v", i+1, rec.Outcome)
		}
		if got := rec.Verification.Changed; !reflect.DeepEqual(got, []string{path}) {
			t.Fatalf("attempt %d: changed %v, want only %s", i+1, got, path)
		}
		snap, err := verify.LoadSnapshot(filepath.Join(folder, FileBaseline))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := snap.Dirty["earlier-task.txt"]; !ok || !snap.Taken.After(old.Taken) {
			t.Fatalf("attempt %d did not replace the old baseline: %+v", i+1, snap)
		}
		old = snap
	}
}

func TestRunOnceRecordsChangesAcrossFailedAttempts(t *testing.T) {
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleted=%t", deleted), func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root := t.TempDir()
			if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
			folder := filepath.Join(config.RunsDir(root), "r-test", "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			path := "made.txt"
			action := "printf 'new work\\n' > \"$TEST_CHANGED\""
			if deleted {
				path = "dirty.txt"
				action = "rm \"$TEST_CHANGED\""
				if err := os.WriteFile(filepath.Join(root, path), []byte("original work\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			bin := filepath.Join(t.TempDir(), "lane")
			script := "#!/bin/sh\ncat >/dev/null\nif [ \"$TEST_ATTEMPT\" = 1 ]; then\n" + action + "\nexit 1\nfi\nprintf 'STATUS: done\\n' > \"$TEST_REPORT\"\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			r := &roster.Roster{}
			r.Router.Backend = "rules"
			env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin})}
			task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessEdit,
				Touch: []string{"allowed.txt"}, Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
			t.Setenv("TEST_CHANGED", filepath.Join(root, path))
			t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
			t.Setenv("TEST_ATTEMPT", "1")
			first := runOnce(env, task, &brief.Brief{}, 1, nil)
			if first.Outcome.Outcome != OutcomeFailed || first.Outcome.FailureClass != FailTransient {
				t.Fatalf("first outcome: %+v", first.Outcome)
			}
			stored, err := LoadRecord(folder)
			if err != nil || !reflect.DeepEqual(stored.Verification.Changed, []string{path}) {
				t.Fatalf("first Record: %+v, error: %v", stored, err)
			}
			archiveAttempt(folder, 1)
			t.Setenv("TEST_ATTEMPT", "2")
			second := runOnce(env, task, &brief.Brief{}, 2, nil)
			stored, err = LoadRecord(folder)
			if err != nil {
				t.Fatal(err)
			}
			for _, rec := range []*Record{second, stored} {
				if !reflect.DeepEqual(rec.Verification.Changed, []string{path}) || !reflect.DeepEqual(rec.Verification.TouchViolations, []string{path}) {
					t.Fatalf("retry verification: %+v", rec.Verification)
				}
			}
			if deleted {
				diff := DiffFor(root, second)
				if !strings.Contains(diff, "-original work") || !strings.Contains(diff, "+++ /dev/null") {
					t.Fatalf("Task diff lacks removal: %s", diff)
				}
			}
		})
	}
}

func TestRunOnceRecordsChangesOnBlockedCancelStallAndJudgedPaths(t *testing.T) {
	fastDispatchTimers(t)
	for _, mode := range []string{"blocked", "cancel", "stall", "judged"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root, folder := t.TempDir(), t.TempDir()
			if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v %s", err, out)
			}
			bin := filepath.Join(t.TempDir(), "lane")
			script := "#!/bin/sh\nread pointer\nprintf 'work\\n' > \"$TEST_CHANGED\"\n"
			wantOutcome := OutcomeUnverified
			switch mode {
			case "blocked":
				script += "printf 'STATUS: blocked\\n' > \"$TEST_REPORT\"\n"
				wantOutcome = OutcomeBlocked
			case "cancel", "stall":
				if mode == "stall" {
					script += "printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"id\":\"a\",\"type\":\"command_execution\"}}'\n"
				}
				script += "exec sleep 30\n"
				wantOutcome = OutcomeCancelled
				if mode == "stall" {
					wantOutcome = OutcomeTimedOut
				}
			default:
				script += "printf 'STATUS: done\\n' > \"$TEST_REPORT\"\n"
			}
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			changedPath := filepath.Join(root, "made.txt")
			t.Setenv("TEST_CHANGED", changedPath)
			t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
			r := &roster.Roster{}
			r.Router.Backend = "rules"
			env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin})}
			task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessEdit,
				Touch: []string{"made.txt"}, Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
			var clockDone chan error
			if mode == "stall" {
				task.IdleLimit = "1ms"
				clock := newToolClock(t)
				clockDone = make(chan error, 1)
				cancel := make(chan struct{})
				task.Cancel = cancel
				go func() {
					err := clock.drive("idle-after-output", "")
					if err != nil {
						close(cancel)
					}
					clockDone <- err
				}()
			}
			if mode == "cancel" {
				cancel := make(chan struct{})
				task.Cancel = cancel
				go func() {
					defer close(cancel)
					deadline := time.NewTimer(5 * time.Second)
					defer deadline.Stop()
					ticker := time.NewTicker(10 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-deadline.C:
							return
						case <-ticker.C:
							if _, err := os.Stat(changedPath); err == nil {
								return
							}
						}
					}
				}()
			}
			rec := runOnce(env, task, &brief.Brief{}, 1, nil)
			if clockDone != nil {
				if err := <-clockDone; err != nil {
					t.Fatal(err)
				}
			}
			if rec.Outcome.Outcome != wantOutcome {
				t.Fatalf("outcome: %+v, want %s", rec.Outcome, wantOutcome)
			}
			stored, err := LoadRecord(folder)
			if err != nil {
				t.Fatal(err)
			}
			for _, rec := range []*Record{rec, stored} {
				if !reflect.DeepEqual(rec.Verification.Changed, []string{"made.txt"}) {
					t.Fatalf("changed = %v", rec.Verification.Changed)
				}
			}
		})
	}
}

func TestRunOncePausesLimitsAndRecordsRelay(t *testing.T) {
	fastDispatchTimers(t)
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "lane")
	t.Setenv("TEST_RELAY_RELEASE", filepath.Join(folder, "release"))
	script := "#!/bin/sh\nread pointer\nwhile [ ! -f \"$TEST_RELAY_RELEASE\" ]; do sleep 0.002; done\nprintf '## Answer\\nanswer\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: &roster.Roster{}, Project: &config.Project{Allow: []string{"Bash(go run *)"}}, Reg: harness.New(map[string]string{"claude": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessRead, Route: &router.Route{Harness: "claude"}, WallLimit: "100ms", IdleLimit: "100ms"}
	control := controlFolder(t, folder)
	done := make(chan error, 1)
	go func() {
		until := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(folder, FileRecord)); err == nil {
				break
			}
			if time.Now().After(until) {
				done <- fmt.Errorf("Lane did not start")
				return
			}
			time.Sleep(time.Millisecond)
		}
		request := RelayRequest{ID: "0123456789abcdef", ToolName: "Bash", ToolInput: []byte(`{"command":"pwd"}`), AskedAt: time.Now().UTC()}
		if err := writeRelayJSON(control, "relay.json", request); err != nil {
			done <- err
			return
		}
		// Keep the Relay open past both injected limits.
		time.Sleep(160 * time.Millisecond)
		if err := writeRelayJSON(control, "relay-answer-"+request.ID+".json", relayAnswer{request, "deny", time.Now().UTC()}); err != nil {
			done <- err
			return
		}
		if err := os.Remove(filepath.Join(control, "relay.json")); err != nil {
			done <- err
			return
		}
		done <- os.WriteFile(filepath.Join(folder, "release"), nil, 0o600)
	}()
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rec.Outcome.Timer != "" || rec.Outcome.Outcome != OutcomeAnswered {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	var kinds []string
	for _, event := range rec.Events {
		if strings.HasPrefix(event.Kind, "relay") {
			kinds = append(kinds, event.Kind)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"relay", "relay-answer"}) || len(rec.Permission.Relays) != 1 {
		t.Fatalf("Relay events: %v, records: %s", kinds, rec.Permission.Relays)
	}
}

// The fake Harness runs in a separate process, reads the one-shot Lane policy,
// and waits for a real Relay answer before it writes its Report.
func TestOneShotRelayHarness(t *testing.T) {
	if os.Getenv("MAESTRO_TEST_RELAY_HELPER") != "1" {
		return
	}
	folder := os.Getenv("MAESTRO_TASK_FOLDER")
	control := controlFolder(t, folder)
	lane, ok := hook.LoadLane(control)
	if !ok || lane.Access != brief.AccessEdit {
		t.Fatalf("expected a one-shot edit Lane: %+v", lane)
	}
	call := &hook.Call{Harness: lane.Harness, Tool: "Bash", CWD: lane.Root, Input: map[string]any{"command": "go run main.go"}}
	if verdict := hook.DecideLane(call, lane); verdict.Kind != "relay" {
		t.Fatalf("expected Relay: %+v", verdict)
	}
	relayDeadline, relayPollInterval = 350*time.Millisecond, time.Millisecond
	input, err := json.Marshal(call.Input)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	answer, err := RelayWait(control, RelayRequest{ToolName: call.Tool, ToolInput: input})
	if err != nil {
		t.Fatal(err)
	}
	if answer == "denied: no answer" && time.Since(started) < relayDeadline {
		t.Fatal("Relay denied before its deadline")
	}
	if err := os.WriteFile(filepath.Join(folder, FileReport), []byte("## Summary\n"+answer+"\n\nSTATUS: done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestRunOnceEditRelayAnswerAndDeadline(t *testing.T) {
	fastDispatchTimers(t)
	// The cases run in parallel: each case has its own project, control folder,
	// and Task id, and the environment and the Solo helper below are the same
	// for every case.
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TEST_RELAY_HELPER", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_RELAY_EXE", self)
	client, callsPath := deferredSoloClient(t)
	for i, harnessName := range []string{"claude", "agy"} {
		for j, answer := range []string{"allow", "timeout"} {
			id := fmt.Sprintf("b%03x", 2*i+j)
			t.Run(harnessName+"/"+answer, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, out)
				}
				folder := filepath.Join(config.RunsDir(root), "r-test", id)
				if err := os.MkdirAll(folder, 0o755); err != nil {
					t.Fatal(err)
				}
				bin := filepath.Join(t.TempDir(), "lane")
				script := "#!/bin/sh\nexec \"$MAESTRO_TEST_RELAY_EXE\" -test.run=^TestOneShotRelayHarness$ -- \"$@\"\n"
				if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				r := &roster.Roster{Harness: map[string]config.Harness{harnessName: {}}}
				r.Router.Backend = "rules"
				env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{harnessName: bin}),
					Solo: &SoloLink{Client: client, ProjectID: 1, Orchestrator: 42}}
				task := &Task{Project: root, Folder: folder, Run: "r-test", Task: id, Access: brief.AccessEdit,
					Route: &router.Route{Harness: harnessName}, WallLimit: "200ms", IdleLimit: "200ms"}
				control := controlFolder(t, folder)
				reply := make(chan error, 1)
				go func() {
					request, err := waitRelayRequest(control)
					if err == nil && answer == "allow" {
						// Stay silent longer than both limits, then use the same
						// answer file as `maestro answer`.
						time.Sleep(250 * time.Millisecond)
						err = WriteRelayAnswer(control, "allow", request.ID)
					}
					reply <- err
				}()
				rec := runOnce(env, task, &brief.Brief{}, 1, nil)
				if err := <-reply; err != nil {
					t.Fatal(err)
				}
				if rec.Outcome.Timer != "" || rec.Outcome.ExitCode != 0 || rec.Outcome.Outcome != OutcomeUnverified || rec.Outcome.Status != brief.StatusDone {
					t.Fatalf("Lane did not finish after Relay: %+v; stderr: %s", rec.Outcome, tail(filepath.Join(folder, FileStderr), 1000))
				}
				var kinds []string
				for _, event := range rec.Events {
					if strings.HasPrefix(event.Kind, "relay") {
						kinds = append(kinds, event.Kind)
					}
				}
				if !reflect.DeepEqual(kinds, []string{"relay", "relay-answer"}) || len(rec.Permission.Relays) != 1 {
					t.Fatalf("Relay events: %v, records: %s", kinds, rec.Permission.Relays)
				}
				var saved struct {
					Answer  string
					Seconds float64
				}
				if err := json.Unmarshal(rec.Permission.Relays[0], &saved); err != nil {
					t.Fatal(err)
				}
				want, seconds := "allow", 0.25
				if answer == "timeout" {
					// RelayWait starts its deadline before it publishes AskedAt.
					want, seconds = "denied: no answer", 0.34
				}
				if saved.Answer != want || saved.Seconds < seconds {
					t.Fatalf("Relay answer: %+v, want %q after at least %.0fs", saved, want, seconds)
				}
				report, err := os.ReadFile(filepath.Join(folder, FileReport))
				if err != nil || !strings.Contains(string(report), "\n"+want+"\n") {
					t.Fatalf("Harness did not receive the answer: %s, %v", report, err)
				}
				all, err := os.ReadFile(callsPath)
				var calls string
				for _, line := range strings.SplitAfter(string(all), "\n") {
					if strings.Contains(line, `id=\"`+id+`\"`) {
						calls += line
					}
				}
				if err != nil || strings.Count(calls, `"name":"timer_set"`) != 1 || strings.Contains(string(all), `"name":"spawn_agent"`) || !strings.Contains(calls, "Maestro relay:") {
					t.Fatalf("Solo Relay wake-up: %s, %v", all, err)
				}
			})
		}
	}
}

func TestArchiveAttemptPreservesBaselineAndLane(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := t.TempDir()
	control := controlFolder(t, folder)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{FileBaseline} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"lane.json", "answer-allow", "answer-deny", "relay.json", "relay.lock", "relay-answer-0123456789abcdef.json", "relay-answer-fedcba9876543210.json"} {
		if err := os.WriteFile(filepath.Join(control, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	archiveAttempt(folder, 1)
	for _, name := range []string{FileBaseline, "lane.json", "answer-allow", "answer-deny", "relay.json", "relay.lock", "relay-answer-0123456789abcdef.json", "relay-answer-fedcba9876543210.json"} {
		b, err := os.ReadFile(filepath.Join(folder, FileAttempts, "1", name))
		if err != nil || string(b) != name {
			t.Fatalf("archive %s: %q, %v", name, b, err)
		}
		if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("archive left %s in the Task folder: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(control, name)); !os.IsNotExist(err) {
			t.Fatalf("archive left %s in the control folder: %v", name, err)
		}
	}
}

func TestClearAnswersRemovesLane(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := controlFolder(t, filepath.Join(t.TempDir(), "r-test", "abcd"))
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	names := []string{"lane.json", "relay.lock", "relay.json", "relay-answer-0123456789abcdef.json", "relay-answer-fedcba9876543210.json", "answer-allow", "answer-deny"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(`{"access":"edit"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clearAnswers(folder)
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("control file %s remains after cleanup: %v", name, err)
		}
	}
}

func TestRunOnceWritesLaneForEveryHarnessAndAccess(t *testing.T) {
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		for _, access := range []string{brief.AccessRead, brief.AccessEdit} {
			t.Run(name+"/"+access, func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				root := t.TempDir()
				folder := filepath.Join(t.TempDir(), "r-test", "abcd")
				if err := os.MkdirAll(folder, 0o755); err != nil {
					t.Fatal(err)
				}
				if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, out)
				}
				bin := filepath.Join(t.TempDir(), "lane")
				control := controlFolder(t, folder)
				if !config.IsProjectKey(filepath.Base(filepath.Dir(control))) || filepath.Base(control) != "abcd" {
					t.Fatalf("control folder = %s", control)
				}
				t.Setenv("MAESTRO_TEST_CONTROL", control)
				script := "#!/bin/sh\ncat >/dev/null\n[ -s \"$MAESTRO_TEST_CONTROL/lane.json\" ] || exit 7\nprintf '## Answer\\nfile report\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
				if name == "agy" {
					if access == brief.AccessRead {
						script = "#!/bin/sh\n[ -s \"$MAESTRO_TEST_CONTROL/lane.json\" ] || exit 7\nprintf '%s\\n' '{\"status\":\"SUCCESS\",\"response\":\"## Answer\\nresponse report\\nSTATUS: done\\n\",\"usage\":{\"input_tokens\":12,\"output_tokens\":4}}'\n"
					} else {
						script += "printf '%s\\n' '{\"status\":\"SUCCESS\",\"response\":\"This final message is not a Report\",\"usage\":{\"input_tokens\":12,\"output_tokens\":4}}'\n"
					}
				}
				if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				project := &config.Project{Allow: []string{"Bash(go run *)"}, Checks: []string{"git status --short"}}
				env := &Env{Root: root, Roster: &roster.Roster{}, Project: project, Reg: harness.New(map[string]string{name: bin})}
				task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: access,
					Dirs: []string{t.TempDir()}, Checks: []string{"git diff --name-only"}, Commit: access == brief.AccessEdit,
					Route: &router.Route{Harness: name}, WallLimit: "1m", IdleLimit: "1m"}
				for attempt := 1; attempt <= 2; attempt++ {
					want := &hook.Lane{Task: task.Task, Harness: name, Access: access, Root: root, Dirs: task.Dirs,
						RunFolder: folder, Allow: []string{}, Checks: []string{}, Commit: task.Commit}
					if access == brief.AccessEdit {
						want.Allow = project.Allow
						want.Checks = []string{"git status --short", "git diff --name-only"}
					}
					rec := runOnce(env, task, &brief.Brief{}, attempt, nil)
					if rec.Outcome.Outcome != OutcomeAnswered && rec.Outcome.Outcome != OutcomeUnverified {
						t.Fatalf("outcome: %+v", rec.Outcome)
					}
					got, ok := hook.LoadLane(control)
					if !ok || !reflect.DeepEqual(got, want) {
						t.Fatalf("Lane permissions: %+v, want %+v", got, want)
					}
					if st, err := os.Stat(control); err != nil || st.Mode().Perm() != 0o700 {
						t.Fatalf("control folder mode: %v, %v", st, err)
					}
					if _, err := os.Stat(filepath.Join(folder, "lane.json")); !os.IsNotExist(err) {
						t.Fatalf("Lane policy remains in the Task folder: %v", err)
					}
					if name == "agy" {
						report, ok := rec.Report()
						wantText := "file report"
						if access == brief.AccessRead {
							wantText = "response report"
						}
						if !ok || !strings.Contains(report.Raw, wantText) || rec.Outcome.ReportByText != (access == brief.AccessRead) || rec.Budget.Tokens.Input == nil || *rec.Budget.Tokens.Input != 12 {
							t.Fatalf("Antigravity Report and tokens: %+v", rec)
						}
					}
					task.Dirs = []string{t.TempDir()}
					project.Allow = []string{"Bash(go vet *)"}
				}
			})
		}
	}
}

func TestRunOnceAgyUsesOneShotInSolo(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	t.Setenv("MAESTRO_TEST_REPORT", filepath.Join(folder, FileReport))
	client, callsPath := deferredSoloClient(t)
	bin := filepath.Join(t.TempDir(), "agy")
	argsPath := filepath.Join(t.TempDir(), "args")
	t.Setenv("MAESTRO_TEST_ARGS", argsPath)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$MAESTRO_TEST_ARGS\"\nprintf '%s\\n' '{\"status\":\"SUCCESS\",\"response\":\"## Answer\\nPASS\\nPOSITION: confirmed\\n\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: &roster.Roster{Harness: map[string]config.Harness{"agy": {}}},
		Reg: harness.New(map[string]string{"agy": bin}), Solo: &SoloLink{Client: client, ProjectID: 1}}
	task := &Task{Project: root, Folder: folder, Run: "council", Task: "seat", Access: brief.AccessRead, Council: true,
		Route: &router.Route{Harness: "agy", Model: "gemini-3.8-flash", Effort: "high"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{Title: "Council"}, 1, nil)
	if rec.Outcome.Outcome != OutcomeAnswered || !rec.Outcome.ReportByText {
		t.Fatalf("Antigravity one-shot outcome: %+v", rec.Outcome)
	}
	lane, ok := hook.LoadLane(controlFolder(t, folder))
	if !ok || lane.Harness != "agy" || lane.Access != brief.AccessRead {
		t.Fatalf("one-shot permissions: %+v", lane)
	}

	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), `"name":"spawn_process"`) != 0 || strings.Count(string(calls), `"name":"send_input"`) != 0 || strings.Count(string(calls), `"name":"close_process"`) != 0 {
		t.Fatalf("unexpected Solo calls: %s", calls)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	briefRel, _ := filepath.Rel(root, filepath.Join(folder, FileBrief))
	want := []string{"--sandbox", "--output-format", "json", "--model", "gemini-3.8-flash-high", "--print-timeout", "1m0s", "--disable-slash-commands", "-p", brief.Pointer(briefRel)}
	if !reflect.DeepEqual(strings.Split(strings.TrimSuffix(string(args), "\n"), "\n"), want) {
		t.Fatalf("one-shot launch: %s, want %q", args, want)
	}
}

func TestRunOnceClaudeReadUsesAbsolutePaths(t *testing.T) {
	for _, inSolo := range []bool{false, true} {
		t.Run(fmt.Sprintf("solo=%t", inSolo), func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			root := t.TempDir()
			folder := filepath.Join(config.RunsDir(root), "r-test", "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(t.TempDir(), "lane")
			pointerPath := filepath.Join(folder, "received-pointer.txt")
			cwdPath := filepath.Join(folder, "received-cwd.txt")
			t.Setenv("MAESTRO_TEST_POINTER", pointerPath)
			t.Setenv("MAESTRO_TEST_CWD", cwdPath)
			script := "#!/bin/sh\npwd > \"$MAESTRO_TEST_CWD\"\ncat > \"$MAESTRO_TEST_POINTER\"\nprintf '## Answer\\nanswer\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			env := &Env{Root: root, Roster: &roster.Roster{Harness: map[string]config.Harness{"claude": {}}}, Reg: harness.New(map[string]string{"claude": bin})}
			if inSolo {
				client, _ := deferredSoloClient(t)
				env.Solo = &SoloLink{Client: client, ProjectID: 1}
			}
			task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessRead,
				Route: &router.Route{Harness: "claude", Model: "opus"}, WallLimit: "1m", IdleLimit: "1m"}
			absoluteFile := filepath.Join(root, "absolute.go")
			b := &brief.Brief{Files: []string{"main.go", absoluteFile}}
			rec := runOnce(env, task, b, 1, nil)
			if rec.Outcome.Outcome != OutcomeAnswered {
				t.Fatalf("outcome: %+v", rec.Outcome)
			}
			data, err := os.ReadFile(pointerPath)
			if err != nil {
				t.Fatal(err)
			}
			pointer := string(data)
			cwd, err := os.ReadFile(cwdPath)
			resolvedFolder, resolveErr := filepath.EvalSymlinks(folder)
			if err != nil || resolveErr != nil || strings.TrimSpace(string(cwd)) != resolvedFolder {
				t.Fatalf("working directory=%q, errors=%v/%v", cwd, err, resolveErr)
			}
			if !strings.HasPrefix(pointer, brief.Pointer(filepath.Join(folder, FileBrief))) || !strings.Contains(pointer, root) {
				t.Fatalf("Pointer: %s", pointer)
			}
			rendered, err := os.ReadFile(filepath.Join(folder, FileBrief))
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{filepath.Join(root, "main.go"), absoluteFile, filepath.Join(folder, FileReport)} {
				if !filepath.IsAbs(path) || !strings.Contains(string(rendered), path) {
					t.Fatalf("missing absolute path %s in Brief: %s", path, rendered)
				}
			}
			if !reflect.DeepEqual(b.Files, []string{"main.go", absoluteFile}) {
				t.Fatalf("source Brief changed: %v", b.Files)
			}
		})
	}
}

func TestRunOnceRecordsClaudeAutoEnforcement(t *testing.T) {
	for _, auto := range []bool{false, true} {
		t.Run(fmt.Sprintf("auto=%t", auto), func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root := t.TempDir()
			if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, out)
			}
			folder := filepath.Join(config.RunsDir(root), "r-test", "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(t.TempDir(), "lane")
			script := "#!/bin/sh\ncat >/dev/null\nprintf 'STATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"claude": bin})}
			task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessEdit, ClaudeAuto: auto,
				Route: &router.Route{Harness: "claude", Enforcement: harness.Enforced}, WallLimit: "1m", IdleLimit: "1m"}
			rec := runOnce(env, task, &brief.Brief{}, 1, nil)
			if rec.Outcome.Outcome != OutcomeUnverified {
				t.Fatalf("outcome: %+v", rec.Outcome)
			}
			stored, err := LoadRecord(folder)
			if err != nil {
				t.Fatal(err)
			}
			want := harness.Enforced
			if auto {
				want = harness.BestEffort
			}
			for _, record := range []*Record{rec, stored} {
				if record.Permission.Enforcement != want {
					t.Fatalf("Record Enforcement=%q, want %q", record.Permission.Enforcement, want)
				}
			}
		})
	}
}

func TestAttemptDiffUsesDirtyFileContentsAtStart(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	path := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(path, []byte("earlier Task\nold line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("earlier Task\nnew line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := snap.diff(root, []string{"shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-old line") || !strings.Contains(diff, "+new line") || strings.Contains(diff, "+earlier Task") {
		t.Fatalf("diff includes work before the attempt: %s", diff)
	}
	rec := &Record{Path: folder, Verification: Verification{Changed: []string{"shared.txt"}}}
	if got := DiffFor(root, rec); got != diff {
		t.Fatalf("saved baseline diff = %q, want %q", got, diff)
	}
}

func TestEditPreflightRequiresGitBeforeRouting(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			if missing {
				t.Setenv("PATH", t.TempDir())
			}
			env := &Env{Root: t.TempDir(), Roster: &roster.Roster{}}
			b := &brief.Brief{Access: brief.AccessEdit}
			if _, err := Prepare(env, b, router.Request{}, "", ""); err == nil || !strings.Contains(err.Error(), "snapshot needs git") {
				t.Fatalf("preflight error: %v", err)
			}
		})
	}
}

func TestReadDispatchWorksWithoutGitOrRepository(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\nprintf '## Answer\\nanswer\\nSTATUS: done\\n' > \"$TEST_REPORT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
	env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"codex": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "read", Access: brief.AccessRead, Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.Outcome != OutcomeAnswered {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
}

func TestUnknownFullClaudeModelKeepsCLIError(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	message := "There is an issue with the selected model (claude-opuss-5-5). It may not exist or you may not have access to it."
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '" + message + "' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"claude": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "bad-model", Access: brief.AccessRead,
		Route: &router.Route{Harness: "claude", Model: "claude-opuss-5-5"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.FailureClass != FailLane || rec.Outcome.ErrorText != message {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	stored, err := LoadRecord(folder)
	if err != nil || stored.Outcome.FailureClass != FailLane || stored.Outcome.ErrorText != message {
		t.Fatalf("stored record=%+v err=%v", stored, err)
	}
}
