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
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func wakeBodies(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var call struct {
			Params struct {
				Name      string
				Arguments struct {
					Body      string
					ProjectID int `json:"project_id"`
					ProcessID int `json:"delivery_process_id"`
					DelayMS   int `json:"delay_ms"`
				}
			}
		}
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name == "timer_set" {
			args := call.Params.Arguments
			if args.ProjectID != 1 || args.ProcessID != 42 || args.DelayMS != 1000 || strings.Contains(args.Body, "\n") {
				t.Fatalf("wake-up: %s", line)
			}
			bodies = append(bodies, args.Body)
		}
	}
	return bodies
}

func TestSoloDispatchEndWake(t *testing.T) {
	fastDispatchTimers(t)
	// The cases run in parallel: each case has its own project and Task id, and
	// the environment and the Solo helper below are the same for every case.
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	client, calls := deferredSoloClient(t)
	for i, tc := range []struct {
		name, status, outcome string
		edit, wake            bool
	}{
		{"failed", "partial", OutcomeFailed, false, true},
		{"blocked", "blocked", OutcomeBlocked, false, true},
		{"timed out", "", OutcomeTimedOut, false, true},
		{"unshaped read", "unshaped", OutcomeAnswered, false, true},
		{"unshaped edit", "unshaped", OutcomeUnverified, true, true},
		{"verified", "done", OutcomeVerified, true, false},
		{"answered", "done", OutcomeAnswered, false, false},
		{"unverified", "done", OutcomeUnverified, true, false},
	} {
		id := fmt.Sprintf("a%03x", i)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, out)
			}
			lane := filepath.Join(t.TempDir(), "lane")
			script := "#!/bin/sh\ncat >/dev/null\n"
			if tc.outcome == OutcomeTimedOut {
				script += "exec sleep 10\n"
			} else {
				script += "printf 'work\\n' > work.txt\nprintf '## Answer\\nTask complete\\nSTATUS: %s\\n' '" + tc.status + "' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
			}
			if err := os.WriteFile(lane, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			r := &roster.Roster{Router: config.Router{Backend: "rules"}}
			off := false
			r.Failure.KeepAwake = &off
			if tc.outcome == OutcomeVerified {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = io.WriteString(w, `{"answers":{"satisfies":{"noul":1},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}}}}`)
				}))
				defer server.Close()
				r.Router = config.Router{Backend: "jev", BaseURL: server.URL}
			}
			access := brief.AccessRead
			if tc.edit {
				access = brief.AccessEdit
			}
			b := &brief.Brief{Path: filepath.Join(root, "source.md"), Title: "Test wake-up", Access: access, Goal: "Finish the Task.", Done: "The Report is complete."}
			if err := brief.Write(b, b.Path); err != nil {
				t.Fatal(err)
			}
			folder := filepath.Join(config.RunsDir(root), "r-test", id)
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			task := &Task{Project: root, Folder: folder, Run: "r-test", Task: id, Title: b.Title, BriefPath: b.Path, Access: access,
				Route: &router.Route{Family: &roster.Family{}, Harness: "codex", FamilyID: "codex-sol"}, MaxAttempt: 1, WallLimit: "1m", IdleLimit: "1m"}
			if tc.outcome == OutcomeTimedOut {
				task.WallLimit = "1ms"
			}
			if tc.outcome == OutcomeVerified {
				task.Checks = []string{"true"}
			}
			env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": lane}), Solo: &SoloLink{Client: client, ProjectID: 1, Orchestrator: 42}}
			rec, err := Execute(env, task)
			if err != nil || rec.Outcome.Outcome != tc.outcome || rec.Outcome.Status != tc.status {
				t.Fatalf("record=%+v error=%v", rec, err)
			}
			var bodies []string
			for _, body := range wakeBodies(t, calls) {
				if strings.Contains(body, id) {
					bodies = append(bodies, body)
				}
			}
			want := 0
			if tc.wake {
				want = 1
			}
			if len(bodies) != want {
				t.Fatalf("wake-ups=%q, want %d", bodies, want)
			}
			if want == 1 && bodies[0] != `Maestro dispatch end: id="`+id+`" title="Test wake-up" harness="codex". Run: maestro wait `+id {
				t.Fatalf("wake-up=%q", bodies[0])
			}
		})
	}
}

func TestSoloDispatchRetryWakesOnlyAfterFinalAttempt(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	client, calls := deferredSoloClient(t)
	root, folder := t.TempDir(), t.TempDir()
	b := &brief.Brief{Path: filepath.Join(root, "brief.md"), Title: "Retry", Access: brief.AccessRead, Goal: "Read.", Done: "Answer."}
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	off := false
	r := &roster.Roster{}
	r.Failure.KeepAwake = &off
	env := &Env{Root: root, Roster: r, Reg: harness.New(nil), Solo: &SoloLink{Client: client, ProjectID: 1, Orchestrator: 42}, RetryDelay: time.Millisecond}
	task := &Task{Project: root, Folder: folder, Task: "abcd", Title: b.Title, BriefPath: b.Path, Access: brief.AccessRead,
		Route: &router.Route{Family: &roster.Family{}, Harness: "absent"}, MaxAttempt: 2}
	rec, err := Execute(env, task)
	if err != nil || rec.Attempt != 2 || rec.Outcome.Outcome != OutcomeFailed {
		t.Fatalf("record=%+v error=%v", rec, err)
	}
	if bodies := wakeBodies(t, calls); len(bodies) != 1 {
		t.Fatalf("wake-ups=%q", bodies)
	}
}

func TestSoloDispatchResolvedDuringDelayDoesNotWake(t *testing.T) {
	client, calls := deferredSoloClient(t)
	folder := t.TempDir()
	rec := &Record{Path: folder, State: "ended", Attempt: 1, EndedAt: time.Now(),
		Outcome: Outcome{Outcome: OutcomeFailed, Status: brief.StatusPartial}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	oldWait := soloWakeWait
	t.Cleanup(func() { soloWakeWait = oldWait })
	soloWakeWait = func(delay time.Duration) {
		if delay != soloWakeDelay {
			t.Errorf("wake-up delay = %s, want %s", delay, soloWakeDelay)
		}
		current := *rec
		current.Outcome = Outcome{Outcome: OutcomeAnswered, Status: brief.StatusDone}
		done <- current.Save()
	}
	link := &SoloLink{Client: client, ProjectID: 1, Orchestrator: 42}
	link.dispatchEnded(&Task{Task: "abcd", Folder: folder, Route: &router.Route{}}, rec)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if bodies := wakeBodies(t, calls); len(bodies) != 0 {
		t.Fatalf("resolved Dispatch woke the Orchestrator: %q", bodies)
	}
}

func TestSoloRelayAnsweredDuringDelayDoesNotWake(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	client, calls := deferredSoloClient(t)
	folder := t.TempDir()
	control := controlFolder(t, folder)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	request := RelayRequest{ID: "current", AskedAt: time.Now(), ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"pwd"}`)}
	if err := writeRelayJSON(control, "relay.json", request); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayJSON(control, "relay.lock", relayLock{PID: os.Getpid(), At: time.Now(), ID: request.ID}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	oldWait := soloWakeWait
	t.Cleanup(func() { soloWakeWait = oldWait })
	soloWakeWait = func(delay time.Duration) {
		if delay != soloWakeDelay {
			t.Errorf("wake-up delay = %s, want %s", delay, soloWakeDelay)
		}
		done <- WriteRelayAnswer(control, "allow", request.ID)
	}
	tracker := &relayTracker{}
	env := &Env{Solo: &SoloLink{Client: client, ProjectID: 1, Orchestrator: 42}}
	task, rec := &Task{Task: "abcd", Folder: folder}, &Record{Path: folder}
	tracker.poll(env, task, rec, "claude", time.Now())
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	tracker.poll(env, task, rec, "claude", time.Now())
	if bodies := wakeBodies(t, calls); len(bodies) != 0 {
		t.Fatalf("resolved Relay woke the Orchestrator: %q", bodies)
	}
}
