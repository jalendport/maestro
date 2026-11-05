package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/run"
)

func waitFixture(t *testing.T) (*dispatch.Record, *run.State) {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "")
	t.Setenv("PATH", "")
	root := t.TempDir()
	t.Chdir(root)
	if err := (&discovery.Cache{RefreshedAt: time.Now()}).Save(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
	for _, path := range []string{folder, filepath.Join(filepath.Dir(folder), "control"), controlFolder(t, folder)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	rec := &dispatch.Record{Run: "r-0001", Task: "abcd", Project: root, Path: folder, State: "running", Pid: os.Getpid(), StartedAt: time.Now(), Route: &router.Route{FamilyID: "claude-fable", Effort: "high"}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	s := &run.State{Run: rec.Run, Project: root, Folder: filepath.Dir(folder), State: run.StateLive, Pid: os.Getpid(), StartedAt: time.Now(), Tasks: []*run.TaskState{{Number: 1, ID: rec.Task, Folder: folder, State: run.TaskRunning}}}
	return rec, s
}

func waitCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	command := (&app{}).build()
	command.SetArgs(args)
	var result error
	output := captureStdout(t, func() error {
		result = command.Execute()
		return nil
	})
	return output, result
}

func requireWaitItem(t *testing.T, output string, err error, asJSON bool, kind, task, runID, command string) waitItem {
	t.Helper()
	if code, ok := proc.Code(err); !ok || code != proc.CodeOrchestrator {
		t.Fatalf("wait error = %v; output = %s", err, output)
	}
	var item waitItem
	if asJSON {
		decoder := json.NewDecoder(strings.NewReader(output))
		if err := decoder.Decode(&item); err != nil {
			t.Fatal(err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			t.Fatalf("wait printed more than one object: %q", output)
		}
		if item.Kind != kind || item.Task != task || item.Run != runID || !strings.Contains(item.NextCommand, command) || item.Reason == "" {
			t.Fatalf("wait item = %+v", item)
		}
	} else if !strings.Contains(output, kind) || !strings.Contains(output, task) || !strings.Contains(output, runID) || !strings.Contains(output, command) {
		t.Fatalf("wait output = %q", output)
	}
	return item
}

func TestWaitRelayReturnsBeforeDispatchEndsAndContinuesAfterAnswer(t *testing.T) {
	for _, onRun := range []bool{false, true} {
		for _, asJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("run=%v/json=%v", onRun, asJSON), func(t *testing.T) {
				rec, s := waitFixture(t)
				rec.Events = []dispatch.Event{{Kind: "relay", Text: "old-relay Bash pwd"}, {Kind: "relay-answer", Text: "allow"}}
				if err := rec.Save(); err != nil {
					t.Fatal(err)
				}
				id, runID := rec.Task, ""
				if onRun {
					id, runID = s.Run, s.Run
					if err := s.Save(); err != nil {
						t.Fatal(err)
					}
				}
				control := controlFolder(t, rec.Path)
				opened := make(chan time.Time, 1)
				done := make(chan error, 1)
				go func() {
					time.Sleep(100 * time.Millisecond)
					opened <- time.Now()
					_, err := dispatch.RelayWait(control, dispatch.RelayRequest{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make deploy"}`)})
					if err == nil {
						rec.State = "ended"
						rec.Outcome = dispatch.Outcome{Status: "done", Outcome: dispatch.OutcomeAnswered}
						err = rec.Save()
					}
					if onRun && err == nil {
						s.State, s.Tasks[0].State = run.StateFinished, run.TaskDone
						err = s.Save()
					}
					done <- err
				}()
				// End the fixture even if wait incorrectly waits past the Relay.
				fallback := time.AfterFunc(4*time.Second, func() { <-answerOpenRelay(control, "deny") })
				t.Cleanup(func() {
					fallback.Stop()
					var request dispatch.RelayRequest
					b, _ := os.ReadFile(filepath.Join(control, "relay.json"))
					if json.Unmarshal(b, &request) == nil {
						_ = dispatch.WriteRelayAnswer(control, "deny", request.ID)
					}
				})
				args := []string{"wait", id}
				if asJSON {
					args = append(args, "--json")
				}
				output, err := waitCommand(t, args...)
				elapsed := time.Since(<-opened)
				requireWaitItem(t, output, err, asJSON, "relay", "abcd", runID, "maestro answer abcd allow|deny --relay ")
				if elapsed >= 3*time.Second {
					t.Fatalf("wait returned %s after the Relay opened", elapsed)
				}
				fallback.Stop()
				live, err := dispatch.LoadRecord(rec.Path)
				if err != nil || live.State != "running" {
					t.Fatalf("Dispatch already ended: %+v, %v", live, err)
				}
				var request dispatch.RelayRequest
				b, err := os.ReadFile(filepath.Join(control, "relay.json"))
				if err != nil || json.Unmarshal(b, &request) != nil {
					t.Fatalf("Relay request: %q, %v", b, err)
				}
				if !strings.Contains(output, request.ID) {
					t.Fatalf("output lacks Relay id: %q", output)
				}
				if _, err := waitCommand(t, "answer", "abcd", "allow", "--relay", request.ID); err != nil {
					t.Fatal(err)
				}
				output, err = waitCommand(t, args...)
				if err != nil {
					t.Fatalf("second wait: %v; %s", err, output)
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if asJSON {
					var result struct{ State string }
					if json.Unmarshal([]byte(output), &result) != nil || (result.State != "ended" && result.State != run.StateFinished) {
						t.Fatalf("second wait did not reach the end: %q", output)
					}
				} else if !strings.Contains(output, "done") && !strings.Contains(output, "finished") {
					t.Fatalf("second wait did not print the report: %q", output)
				}
			})
		}
	}
}

func TestWaitRunPauseAndResume(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(fmt.Sprint(asJSON), func(t *testing.T) {
			_, s := waitFixture(t)
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			paused := make(chan error, 1)
			go func() {
				time.Sleep(100 * time.Millisecond)
				s.State, s.PauseReason, s.Pid = run.StatePaused, "same error twice", 0
				s.Tasks[0].State, s.Tasks[0].Error = run.TaskBlocked, "check failed"
				paused <- s.Save()
			}()
			args := []string{"wait", s.Run}
			if asJSON {
				args = append(args, "--json")
			}
			output, err := waitCommand(t, args...)
			if saveErr := <-paused; saveErr != nil {
				t.Fatal(saveErr)
			}
			requireWaitItem(t, output, err, asJSON, "run_paused", "abcd", s.Run, "maestro resume r-0001 --task 1")
			if !strings.Contains(output, s.PauseReason) {
				t.Fatalf("pause reason absent: %q", output)
			}
			if _, err := waitCommand(t, "resume", s.Run, "--task", "1", "--skip", "--wait"); err != nil {
				t.Fatal(err)
			}
			output, err = waitCommand(t, args...)
			if err != nil || !strings.Contains(output, run.StateFinished) {
				t.Fatalf("wait after resume: %v, %q", err, output)
			}
		})
	}
}

func TestWaitDoesNotReturnForAnsweredRelay(t *testing.T) {
	rec, _ := waitFixture(t)
	control := controlFolder(t, rec.Path)
	request := dispatch.RelayRequest{ID: "answered", AskedAt: time.Now(), ToolName: "Bash"}
	for name, value := range map[string]any{
		"relay.json": request,
		"relay.lock": map[string]any{"pid": os.Getpid(), "at": time.Now(), "id": request.ID},
	} {
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(control, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := dispatch.WriteRelayAnswer(control, "allow", request.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		rec.State = "ended"
		rec.Outcome = dispatch.Outcome{Status: "done", Outcome: dispatch.OutcomeAnswered}
		done <- rec.Save()
	}()
	output, err := waitCommand(t, "wait", rec.Task, "--json")
	if saveErr := <-done; saveErr != nil {
		t.Fatal(saveErr)
	}
	if err != nil || !strings.Contains(output, `"state": "ended"`) {
		t.Fatalf("answered Relay stopped wait: %v, %q", err, output)
	}
}

func TestWaitRunBlockedTaskAndPendingResume(t *testing.T) {
	_, s := waitFixture(t)
	s.Tasks[0].State, s.Tasks[0].Error = run.TaskBlocked, "needs a corrected Brief"
	s.Tasks = append(s.Tasks, &run.TaskState{Number: 2, ID: "def0", State: run.TaskRunning})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	output, err := waitCommand(t, "wait", s.Run, "--json")
	requireWaitItem(t, output, err, true, "task_blocked", "abcd", s.Run, "maestro resume r-0001 --task 1")
	// A live supervisor takes the resume marker on its next poll.
	if err := run.WriteIndex(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { run.RemoveIndex(s.Project, s.Run) })
	if _, err := waitCommand(t, "resume", s.Run, "--task", "1", "--skip"); err != nil {
		t.Fatal(err)
	}
	if item, err := runWaitItem(s); err != nil || item != nil {
		t.Fatalf("queued answer still needs Orchestrator: %+v, %v", item, err)
	}
	s.State, s.PauseReason = run.StatePaused, "needs an answer"
	if item, err := runWaitItem(s); err != nil || item != nil {
		t.Fatalf("queued answer repeated the pause: %+v, %v", item, err)
	}
	// A new blocked Task still needs an answer while the first resumes.
	s.Tasks[1].State, s.Tasks[1].Error = run.TaskBlocked, "new blocked Task"
	item, err := runWaitItem(s)
	if err != nil || item == nil || item.Task != "def0" {
		t.Fatalf("new item was lost: %+v, %v", item, err)
	}
}

func TestWaitKeepsFinalDispatchOutcome(t *testing.T) {
	for _, outcome := range []string{dispatch.OutcomeAnswered, dispatch.OutcomeFailed, dispatch.OutcomeBlocked} {
		t.Run(outcome, func(t *testing.T) {
			rec, _ := waitFixture(t)
			rec.State, rec.Outcome.Outcome = "ended", outcome
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			output, err := waitCommand(t, "wait", rec.Task, "--json")
			code, _ := proc.Code(err)
			want := map[string]int{dispatch.OutcomeAnswered: 0, dispatch.OutcomeFailed: 1, dispatch.OutcomeBlocked: 2}[outcome]
			if code != want || !strings.Contains(output, `"outcome": "`+outcome+`"`) {
				t.Fatalf("final wait: code=%d, want=%d, output=%q", code, want, output)
			}
		})
	}
}
