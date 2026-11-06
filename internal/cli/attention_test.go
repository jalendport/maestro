package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/run"
	"github.com/jalendport/maestro/internal/solo"
)

func gonePID(t *testing.T) int {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestAttentionIgnoresDeadProcesses(t *testing.T) {
	for _, onRun := range []bool{false, true} {
		t.Run(map[bool]string{false: "Dispatch", true: "Run"}[onRun], func(t *testing.T) {
			root, paused, live, _ := liveReminderFixture(t)
			paused.State = run.StateFinished
			reminderJSON(t, filepath.Join(paused.Folder, run.FileState), paused)
			dead := gonePID(t)
			live.Pid = dead
			reminderJSON(t, filepath.Join(live.Folder, run.FileState), live)
			folder := filepath.Join(live.Folder, "cccc")
			rec, err := dispatch.LoadRecord(folder)
			if err != nil {
				t.Fatal(err)
			}
			rec.Pid = dead
			reminderJSON(t, filepath.Join(folder, dispatch.FileRecord), rec)
			id := live.Run
			if !onRun {
				id = rec.Task
				if err := os.Remove(filepath.Join(live.Folder, run.FileState)); err != nil {
					t.Fatal(err)
				}
			}
			if got := run.ReminderItems(root); got != "" {
				t.Fatalf("dead process in Reminder: %q", got)
			}
			t.Chdir(root)
			output, err := waitCommand(t, "status", id)
			if err != nil || !strings.Contains(output, "stale (process gone)") {
				t.Fatalf("status: %v, %s", err, output)
			}
			if onRun {
				if item, err := runWaitItem(live); err != nil || item != nil {
					t.Fatalf("dead Run needs an answer: %+v, %v", item, err)
				}
			} else if item, err := relayItem(folder, rec.Task); err != nil || item != nil {
				t.Fatalf("dead Dispatch needs an answer: %+v, %v", item, err)
			}
		})
	}
}

func TestAttentionFailedTask(t *testing.T) {
	rec, s := waitFixture(t)
	s.Pid = os.Getpid()
	s.Tasks[0].State, s.Tasks[0].Error = run.TaskFailed, "checks failed"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if got := run.ReminderItems(rec.Project); !strings.Contains(got, "failed  run r-0001  task abcd  checks failed") {
		t.Fatalf("failed Task absent: %s", got)
	}
	output, err := waitCommand(t, "wait", s.Run, "--json")
	requireWaitItem(t, output, err, true, "task_failed", rec.Task, s.Run, "maestro resume r-0001 --task 1")
}

func TestAttentionLongRelayInput(t *testing.T) {
	rec, _ := waitFixture(t)
	control := controlFolder(t, rec.Path)
	input, _ := json.Marshal(strings.Repeat("x", 2000))
	reminderJSON(t, filepath.Join(control, "relay.json"), dispatch.RelayRequest{ID: "long", AskedAt: time.Now(), ToolName: "Bash", ToolInput: input})
	reminderJSON(t, filepath.Join(control, "relay.lock"), map[string]any{"id": "long", "pid": os.Getpid(), "at": time.Now()})
	item, err := relayItem(rec.Path, rec.Task)
	if err != nil || item == nil {
		t.Fatalf("Relay: %+v, %v", item, err)
	}
	if len(strings.TrimPrefix(item.Reason, "Bash ")) > 300 {
		t.Fatalf("wait input has %d characters", len(item.Reason)-len("Bash "))
	}
	text := run.ReminderItems(rec.Project)
	_, text, found := strings.Cut(text, "Bash ")
	inputText, _, _ := strings.Cut(text, "  maestro answer")
	if !found || len(inputText) > 300 {
		t.Fatalf("Reminder input has %d characters", len(inputText))
	}
}

func TestAttentionPausedReportPath(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[asJSON], func(t *testing.T) {
			_, s := waitFixture(t)
			s.State, s.Tasks[0].State = run.StatePaused, run.TaskBlocked
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			args := []string{"wait", s.Run}
			if asJSON {
				args = append(args, "--json")
			}
			output, err := waitCommand(t, args...)
			requireWaitItem(t, output, err, asJSON, "run_paused", "abcd", s.Run, "maestro resume")
			path := filepath.Join(s.Folder, run.FileReport)
			if !strings.Contains(output, path) {
				t.Fatalf("report path absent: %s", output)
			}
			if asJSON {
				var item map[string]any
				if err := json.Unmarshal([]byte(output), &item); err != nil || item["report_path"] != path {
					t.Fatalf("report_path: %+v, %v", item, err)
				}
			}
		})
	}
}

func TestAttentionNestedGitProject(t *testing.T) {
	root, _, _, _ := liveReminderFixture(t)
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(config.RunsDir(nested), "r-0010")
	reminderJSON(t, filepath.Join(folder, run.FileState), &run.State{Run: "r-0010", State: run.StatePaused, Folder: folder})
	for _, harness := range []string{"claude", "codex", "agy"} {
		text := reminderText(t, harness, runHookPayload(t, reminderPayload(t, harness, nested), harness))
		if !strings.Contains(text, "paused  run r-0010") || strings.Contains(text, "r-0001") || strings.Contains(text, "r-0002") {
			t.Fatalf("nested project Reminder: %s", text)
		}
	}
}

func TestAttentionResumeWhileStartIsSlow(t *testing.T) {
	rec, s := waitFixture(t)
	s.Pid = 0
	s.Tasks[0].State = run.TaskBlocked
	path := filepath.Join(rec.Project, "corrected.md")
	b := &brief.Brief{Fields: brief.Fields{Title: "Read the project", Access: brief.AccessRead, Level: "some"}, Goal: "Read the project.", Done: "The Report has an answer."}
	if err := brief.Write(b, path); err != nil {
		t.Fatal(err)
	}
	s.Tasks[0].BriefPath, s.Tasks[0].Access = path, brief.AccessRead
	sibling := &run.TaskState{Number: 2, ID: "def0", State: run.TaskWaiting, Access: brief.AccessRead, BriefPath: path, Folder: filepath.Join(s.Folder, "def0"), ResumeLane: "codex-sol:medium"}
	s.Tasks = append(s.Tasks, sibling)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		fmt.Fprint(w, `{"answers":{"lane":{"choice":"ask_orchestrator","confidence":0.95},"spec_is_concrete":{"noul":1}}}`)
	}))
	defer func() { unblock(); server.Close() }()
	t.Setenv("TYPESAFE_API_KEY", "fixture-key")
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	r.Router.Backend, r.Router.BaseURL = "jev", server.URL
	r.Router.Thresholds["jev"]["timeout_s"] = 30
	r.Families = map[string]*roster.Family{"codex-sol": r.Families["codex-sol"]}
	r.Accounts = map[string]config.Account{"codex": r.Accounts["codex"]}
	off := false
	r.Failure.KeepAwake = &off
	r.Failure.MaxDispatches = 1
	finish := filepath.Join(t.TempDir(), "finish")
	t.Setenv("MAESTRO_ATTENTION_FINISH", finish)
	bin := filepath.Join(t.TempDir(), "fake-harness")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nread pointer\nwhile [ ! -f \"$MAESTRO_ATTENTION_FINISH\" ]; do /bin/sleep 0.02; done\nprintf '## Answer\nFixture complete.\nSTATUS: done\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	bf := &budget.File{Accounts: map[string]*budget.Account{}}
	bf.Record("codex", []harness.Window{{Kind: "weekly", Used: .1, ResetsAt: time.Now().Add(time.Hour)}}, budget.SourceManual)
	env := &dispatch.Env{Root: rec.Project, Roster: r, Budget: bf, Reg: harness.New(map[string]string{"codex": bin}),
		Disc: &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{"codex": {Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}}}}}
	// The supervisor takes the resume marker on its next poll.
	t.Cleanup(run.SetPollIntervalForTest(5 * time.Millisecond))
	done := make(chan error, 1)
	go func() { done <- run.Start(env, s.Folder) }()
	t.Cleanup(func() {
		if err := os.WriteFile(finish, nil, 0o600); err != nil {
			t.Error(err)
		}
		unblock()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Minute):
			t.Error("supervisor did not stop")
		}
	})
	deadline := time.Now().Add(time.Minute)
	for {
		if running, err := dispatch.LoadRecord(sibling.Folder); err == nil && running.State == "running" && dispatch.ProcessAlive(running) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture Lane did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := waitCommand(t, "resume", s.Run, "--task", "1", "--brief", path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Minute):
		t.Fatal("start did not reach the Router")
	}
	current, err := run.Load(s.Folder)
	if err != nil {
		t.Fatal(err)
	}
	if run.PendingResume(s.Folder, "") {
		t.Fatal("resume marker was not consumed")
	}
	if item, err := runWaitItem(current); err != nil || item != nil {
		t.Fatalf("answered Task stopped wait during start: %+v, %v", item, err)
	}
	if got := run.ReminderItems(s.Project); strings.Contains(got, "blocked") || strings.Contains(got, "paused") || strings.Contains(got, "task abcd") {
		t.Fatalf("answered Task in Reminder during start: %s", got)
	}
}

// TestAttentionSoloHelper serves only the local Solo fixture.
func TestAttentionSoloHelper(t *testing.T) {
	if os.Getenv("MAESTRO_ATTENTION_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int
			Method string
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(1)
		}
		if request.ID == 0 {
			continue
		}
		var result any = map[string]any{}
		if request.Method == "tools/call" {
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": `{}`}}}
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func TestAttentionCLIResumeDuringPauseDelay(t *testing.T) {
	rec, s := waitFixture(t)
	s.Pid, s.Tasks[0].State = 0, run.TaskBlocked
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_ATTENTION_HELPER", "1")
	t.Setenv("MAESTRO_ATTENTION_BINARY", self)
	helper := filepath.Join(t.TempDir(), "solo-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MAESTRO_ATTENTION_BINARY\" -test.run=^TestAttentionSoloHelper$\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_SOLO_MCP", helper)
	client, err := solo.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	off := false
	r.Failure.KeepAwake = &off
	env := &dispatch.Env{Root: rec.Project, Roster: r, Solo: &dispatch.SoloLink{Client: client, Orchestrator: 42}}
	// The pause wake-up waits until the CLI answer is saved, so the answer always
	// comes during the delay; other wake-ups return at once.
	t.Cleanup(dispatch.SetSoloWakeForTest(time.Millisecond, func(time.Duration) {
		until := time.Now().Add(10 * time.Second)
		for time.Now().Before(until) && !run.PendingResume(s.Folder, "") {
			if current, err := run.Load(s.Folder); err != nil || current.State != run.StatePaused {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}))
	done := make(chan error, 1)
	go func() { done <- run.Start(env, s.Folder) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := run.Load(s.Folder)
		if err == nil && current.State == run.StatePaused {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run did not pause")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The pause is on disk, but the owner is still in its Solo wake-up delay.
	if err := run.Start(env, s.Folder); err == nil || !strings.Contains(err.Error(), "supervisor") {
		t.Fatalf("second supervisor accepted during pause: %v", err)
	}
	output, err := waitCommand(t, "resume", s.Run, "--task", "1", "--skip", "--wait")
	if err != nil || !strings.Contains(output, "the supervisor picks the answer up") {
		t.Fatalf("CLI started another supervisor: %v, %s", err, output)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner did not consume the CLI answer")
	}
	current, err := run.Load(s.Folder)
	if err != nil || current.State != run.StateFinished || current.Tasks[0].State != run.TaskSkipped {
		t.Fatalf("resumed Run: %+v, %v", current, err)
	}
	rows, err := os.ReadFile(config.LedgerFile())
	if err != nil || strings.Count(string(rows), `"state":"finished"`) != 1 {
		t.Fatalf("expected one finished Run: %s, %v", rows, err)
	}
}
