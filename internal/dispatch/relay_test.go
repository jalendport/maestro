package dispatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
)

func controlFolder(t *testing.T, folder string) string {
	t.Helper()
	control, err := config.ControlDirForFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	return control
}

func waitRelayRequest(folder string) (RelayRequest, error) {
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		var request RelayRequest
		b, err := os.ReadFile(filepath.Join(folder, "relay.json"))
		if err == nil && json.Unmarshal(b, &request) == nil && request.ID != "" {
			return request, nil
		}
		time.Sleep(time.Millisecond)
	}
	return RelayRequest{}, fmt.Errorf("Relay did not open")
}

func answerRelay(folder, answer string) <-chan error {
	done := make(chan error, 1)
	go func() {
		request, err := waitRelayRequest(folder)
		if err == nil {
			err = WriteRelayAnswer(folder, answer, request.ID)
		}
		done <- err
	}()
	return done
}

func TestRelayDecisionsAndCleanup(t *testing.T) {
	oldDeadline, oldInterval := relayDeadline, relayPollInterval
	relayDeadline, relayPollInterval = 100*time.Millisecond, time.Millisecond
	t.Cleanup(func() { relayDeadline, relayPollInterval = oldDeadline, oldInterval })
	for _, answer := range []string{"allow", "deny", "timeout"} {
		t.Run(answer, func(t *testing.T) {
			folder := t.TempDir()
			var output bytes.Buffer
			done := make(chan error, 1)
			go func() {
				done <- Relay(folder, strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"pwd"},"tool_use_id":"tool-1"}`), &output)
			}()
			if answer != "timeout" {
				var request RelayRequest
				until := time.Now().Add(time.Second)
				for {
					if b, err := os.ReadFile(filepath.Join(folder, "relay.json")); err == nil && json.Unmarshal(b, &request) == nil {
						break
					}
					if time.Now().After(until) {
						t.Fatal("Relay did not open")
					}
					time.Sleep(time.Millisecond)
				}
				if !config.IsProjectKey(request.ID) {
					t.Fatalf("invalid Relay id: %q", request.ID)
				}
				if err := WriteRelayAnswer(folder, answer, request.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			var decision struct {
				Output struct {
					Event    string `json:"hookEventName"`
					Decision struct {
						Behavior string `json:"behavior"`
					} `json:"decision"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(output.Bytes(), &decision); err != nil {
				t.Fatal(err)
			}
			want := "deny"
			if answer == "allow" {
				want = "allow"
			}
			if decision.Output.Event != "PermissionRequest" || decision.Output.Decision.Behavior != want {
				t.Fatalf("decision: %s", output.String())
			}
			for _, name := range []string{"relay.json", "answer-allow", "answer-deny"} {
				if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
					t.Fatalf("%s remains: %v", name, err)
				}
			}
			var saved relayAnswer
			files, err := filepath.Glob(filepath.Join(folder, "relay-answer-*.json"))
			if err != nil || len(files) != 1 {
				t.Fatalf("answer files: %v, %v", files, err)
			}
			b, err := os.ReadFile(files[0])
			if err != nil || json.Unmarshal(b, &saved) != nil || saved.Request.ToolUseID != "tool-1" || saved.Request.AskedAt.IsZero() {
				t.Fatalf("saved answer: %s, %v", b, err)
			}
			if answer == "timeout" && saved.Answer != "denied: no answer" {
				t.Fatalf("timeout: %+v", saved)
			}
		})
	}
}

func TestRelayWaitAnswersAndCleanup(t *testing.T) {
	for _, answer := range []string{"allow", "deny"} {
		t.Run(answer, func(t *testing.T) {
			folder := t.TempDir()
			request := RelayRequest{ToolName: "run_command", ToolInput: json.RawMessage(`{"CommandLine":"go test ./...","Cwd":"/project"}`)}
			reply := answerRelay(folder, answer)
			got, err := RelayWait(folder, request)
			if err := <-reply; err != nil {
				t.Fatal(err)
			}
			if err != nil || got != answer {
				t.Fatalf("answer: %q, %v; want %q", got, err, answer)
			}
			for _, name := range []string{"relay.json", "answer-allow", "answer-deny"} {
				if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
					t.Fatalf("%s remains: %v", name, err)
				}
			}
			var saved relayAnswer
			files, err := filepath.Glob(filepath.Join(folder, "relay-answer-*.json"))
			if err != nil || len(files) != 1 {
				t.Fatalf("answer files: %v, %v", files, err)
			}
			b, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.Answer != answer || saved.Request.ToolName != request.ToolName || !bytes.Equal(saved.Request.ToolInput, request.ToolInput) || saved.Request.AskedAt.IsZero() || saved.AnsweredAt.Before(saved.Request.AskedAt) {
				t.Fatalf("saved answer: %+v", saved)
			}
		})
	}
}

func TestRelayTrackerRecordsRepeatedRequestsAndAnswers(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := t.TempDir()
	control := controlFolder(t, folder)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	task := &Task{Task: "abcd", Folder: folder}
	rec := &Record{Path: folder, Task: "abcd"}
	env := &Env{}
	tracker := &relayTracker{}
	start := time.Now().UTC()
	for i, answer := range []string{"allow", "deny", "denied: no answer"} {
		request := RelayRequest{ID: fmt.Sprintf("%016x", i+1), ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"pwd"}`), AskedAt: start.Add(time.Duration(i) * 10 * time.Second)}
		if err := writeRelayJSON(control, "relay.json", request); err != nil {
			t.Fatal(err)
		}
		tracker.poll(env, task, rec, "claude", request.AskedAt)
		tracker.poll(env, task, rec, "claude", request.AskedAt.Add(time.Second))
		if !tracker.open() || len(rec.Events) != i*2+1 {
			t.Fatalf("open Relay: %+v", rec.Events)
		}
		if err := writeRelayJSON(control, "relay-answer-"+request.ID+".json", relayAnswer{request, answer, request.AskedAt.Add(5 * time.Second)}); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(control, "relay.json")); err != nil {
			t.Fatal(err)
		}
		tracker.poll(env, task, rec, "claude", request.AskedAt.Add(6*time.Second))
		if tracker.open() || rec.Events[i*2].Kind != "relay" || rec.Events[i*2+1].Kind != "relay-answer" || rec.Events[i*2+1].Text != answer {
			t.Fatalf("events: %+v", rec.Events)
		}
		var saved struct {
			Tool, Input, Answer string
			Seconds             float64
		}
		if err := json.Unmarshal(rec.Permission.Relays[i], &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Tool != "Bash" || saved.Answer != answer || saved.Seconds != 5 {
			t.Fatalf("Relay record: %+v", saved)
		}
	}
	if tracker.total != 15*time.Second {
		t.Fatalf("Relay time: %s", tracker.total)
	}
	saved, err := LoadRecord(folder)
	if err != nil || len(saved.Events) != 6 {
		t.Fatalf("saved events: %v, %v", saved, err)
	}
	if !strings.Contains(Status(saved), "maestro answer abcd allow|deny") {
		t.Fatal("status has no answer command")
	}
}

func TestRelayTrackerHandlesMissingAndFastRequests(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	for _, fast := range []bool{false, true} {
		folder := t.TempDir()
		control := controlFolder(t, folder)
		if err := os.MkdirAll(control, 0o700); err != nil {
			t.Fatal(err)
		}
		task := &Task{Task: "abcd", Folder: folder}
		rec := &Record{Path: folder, Task: "abcd"}
		tracker := &relayTracker{}
		env := &Env{}
		request := RelayRequest{ID: "0123456789abcdef", ToolName: "Bash", ToolInput: json.RawMessage(`{}`), AskedAt: time.Now().UTC()}
		if fast {
			if err := writeRelayJSON(control, "relay-answer-"+request.ID+".json", relayAnswer{request, "allow", request.AskedAt.Add(time.Second)}); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := writeRelayJSON(control, "relay.json", request); err != nil {
				t.Fatal(err)
			}
			tracker.poll(env, task, rec, "claude", request.AskedAt)
			if err := os.Remove(filepath.Join(control, "relay.json")); err != nil {
				t.Fatal(err)
			}
		}
		tracker.poll(env, task, rec, "claude", request.AskedAt.Add(time.Second))
		if len(rec.Events) != 2 || len(rec.Permission.Relays) != 1 || tracker.open() {
			t.Fatalf("events: %+v", rec.Events)
		}
	}
}

func TestRelayWaitSerializesAnswers(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := controlFolder(t, filepath.Join(t.TempDir(), "r-test", "abcd"))
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	oldDeadline, oldInterval := relayDeadline, relayPollInterval
	relayDeadline, relayPollInterval = 3*time.Second, time.Millisecond
	t.Cleanup(func() { relayDeadline, relayPollInterval = oldDeadline, oldInterval })
	type result struct {
		answer string
		err    error
	}
	first, second := make(chan result, 1), make(chan result, 1)
	go func() {
		answer, err := RelayWait(folder, RelayRequest{ToolName: "run_command", ToolUseID: "first"})
		first <- result{answer, err}
	}()
	waitForRequest := func(id string) string {
		t.Helper()
		until := time.Now().Add(time.Second)
		for time.Now().Before(until) {
			b, err := os.ReadFile(filepath.Join(folder, "relay.json"))
			var request RelayRequest
			if err == nil && json.Unmarshal(b, &request) == nil && request.ToolUseID == id {
				return request.ID
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("Relay %s did not open", id)
		return ""
	}
	firstID := waitForRequest("first")
	go func() {
		answer, err := RelayWait(folder, RelayRequest{ToolName: "run_command", ToolUseID: "second"})
		second <- result{answer, err}
	}()
	time.Sleep(20 * time.Millisecond)
	b, err := os.ReadFile(filepath.Join(folder, "relay.json"))
	if err != nil || !strings.Contains(string(b), `"tool_use_id":"first"`) {
		t.Fatalf("second caller replaced first request: %s, %v", b, err)
	}
	if err := WriteRelayAnswer(folder, "allow", firstID); err != nil {
		t.Fatal(err)
	}
	if got := <-first; got.err != nil || got.answer != "allow" {
		t.Fatalf("first answer: %+v", got)
	}
	secondID := waitForRequest("second")
	if firstID == secondID {
		t.Fatal("Relay ids match")
	}
	select {
	case got := <-second:
		t.Fatalf("second caller consumed first answer: %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
	if err := WriteRelayAnswer(folder, "deny", secondID); err != nil {
		t.Fatal(err)
	}
	if got := <-second; got.err != nil || got.answer != "deny" {
		t.Fatalf("second answer: %+v", got)
	}
	for _, name := range []string{"relay.lock", "relay.json", "answer-allow", "answer-deny"} {
		if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("control file %s remains: %v", name, err)
		}
	}
}

func TestRelayWaitLockDeadlineDoesNotConsumeAnswer(t *testing.T) {
	folder := t.TempDir()
	// A live owner's lock inside the Relay deadline is never taken over.
	if err := writeRelayJSON(folder, "relay.lock", relayLock{PID: os.Getpid(), At: time.Now().UTC(), ID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "answer-allow"), []byte("owner"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldDeadline, oldInterval := relayDeadline, relayPollInterval
	relayDeadline, relayPollInterval = 20*time.Millisecond, time.Millisecond
	t.Cleanup(func() { relayDeadline, relayPollInterval = oldDeadline, oldInterval })
	if answer, err := RelayWait(folder, RelayRequest{}); err != nil || answer != "denied: no answer" {
		t.Fatalf("lock wait answer = %q, %v", answer, err)
	}
	for _, name := range []string{"relay.lock", "answer-allow"} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Fatalf("waiting caller removed %s: %v", name, err)
		}
	}
}

func TestRelayWaitRecoversDeadOwner(t *testing.T) {
	command := exec.Command("true")
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	if syscall.Kill(pid, 0) != syscall.ESRCH {
		t.Fatal("test process is still alive")
	}
	folder := t.TempDir()
	if err := writeRelayJSON(folder, "relay.lock", relayLock{PID: pid, At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	reply := answerRelay(folder, "allow")
	oldDeadline, oldInterval := relayDeadline, relayPollInterval
	relayDeadline, relayPollInterval = 100*time.Millisecond, time.Millisecond
	t.Cleanup(func() { relayDeadline, relayPollInterval = oldDeadline, oldInterval })
	start := time.Now()
	if answer, err := RelayWait(folder, RelayRequest{}); err != nil || answer != "allow" {
		t.Fatalf("recovered Relay = %q, %v", answer, err)
	}
	if err := <-reply; err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= relayDeadline {
		t.Fatal("dead owner delayed the Relay until its deadline")
	}
	if _, err := os.Stat(filepath.Join(folder, "relay.lock")); !os.IsNotExist(err) {
		t.Fatalf("recovered lock remains: %v", err)
	}
}

func TestRelayWaitKeepsSuccessorFiles(t *testing.T) {
	folder := t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, err := RelayWait(folder, RelayRequest{})
		done <- err
	}()
	opened, err := waitRelayRequest(folder)
	if err != nil {
		t.Fatal(err)
	}
	successor := relayLock{PID: os.Getpid(), At: time.Now().UTC(), ID: "fedcba9876543210"}
	if err := writeRelayJSON(folder, "relay.lock", successor); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayJSON(folder, "relay.json", RelayRequest{ID: successor.ID}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "answer-deny"), []byte(opened.ID), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if owner, ok := readRelayLock(filepath.Join(folder, "relay.lock")); !ok || owner != successor {
		t.Fatalf("successor lock removed: %+v, %v", owner, ok)
	}
	b, err := os.ReadFile(filepath.Join(folder, "relay.json"))
	var request RelayRequest
	if err != nil || json.Unmarshal(b, &request) != nil || request.ID != successor.ID {
		t.Fatalf("successor request removed: %s, %v", b, err)
	}
}

func TestReadAnswerRemovesStaleMarkers(t *testing.T) {
	for _, stale := range []string{"", "1111111111111111"} {
		folder := t.TempDir()
		if err := os.WriteFile(filepath.Join(folder, "answer-allow"), []byte(stale), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "answer-deny"), []byte("2222222222222222"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := readAnswer(folder, "2222222222222222"); got != "deny" {
			t.Fatalf("stale allow was consumed: %q", got)
		}
		for _, name := range []string{"answer-allow", "answer-deny"} {
			if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
				t.Fatalf("%s remains: %v", name, err)
			}
		}
	}
}

func TestRelayTrackerConsumesAllAnswersBetweenPolls(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := t.TempDir()
	control := controlFolder(t, folder)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	task, rec := &Task{Task: "abcd", Folder: folder}, &Record{Path: folder, Task: "abcd"}
	tracker := &relayTracker{}
	env := &Env{}
	start := time.Now().UTC()
	requests := []RelayRequest{
		{ID: "2222222222222222", ToolName: "first", AskedAt: start},
		{ID: "1111111111111111", ToolName: "second", AskedAt: start.Add(2 * time.Second)},
	}
	// The first request is already open when both answers arrive.
	if err := writeRelayJSON(control, "relay.json", requests[0]); err != nil {
		t.Fatal(err)
	}
	tracker.poll(env, task, rec, "agy", start)
	for i, answer := range []string{"allow", "deny"} {
		request := requests[i]
		if err := writeRelayJSON(control, "relay-answer-"+request.ID+".json", relayAnswer{request, answer, request.AskedAt.Add(time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	// The second owner has not yet removed its request file.
	if err := writeRelayJSON(control, "relay.json", requests[1]); err != nil {
		t.Fatal(err)
	}
	tracker.poll(env, task, rec, "agy", start.Add(4*time.Second))
	tracker.poll(env, task, rec, "agy", start.Add(5*time.Second))
	if tracker.open() || len(rec.Permission.Relays) != 2 || len(rec.Events) != 4 || tracker.total != 2*time.Second {
		t.Fatalf("Relay state: %+v, %+v", tracker, rec)
	}
	for i, want := range []string{"allow", "deny"} {
		var entry struct{ ID, Tool, Answer string }
		if err := json.Unmarshal(rec.Permission.Relays[i], &entry); err != nil {
			t.Fatal(err)
		}
		if entry.ID != requests[i].ID || entry.Tool != requests[i].ToolName || entry.Answer != want {
			t.Fatalf("Relay %d has wrong answer: %+v", i, entry)
		}
	}
	paths, err := filepath.Glob(filepath.Join(control, "relay-answer-*.json"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("unread answers: %v, %v", paths, err)
	}
}

func TestRelayWaitRecoversStaleOwnerByAge(t *testing.T) {
	folder := t.TempDir()
	oldDeadline, oldInterval := relayDeadline, relayPollInterval
	relayDeadline, relayPollInterval = 100*time.Millisecond, time.Millisecond
	t.Cleanup(func() { relayDeadline, relayPollInterval = oldDeadline, oldInterval })
	// The owner pid is alive (this process), but the lock is older than any Relay lives.
	if err := writeRelayJSON(folder, "relay.lock", relayLock{PID: os.Getpid(), At: time.Now().UTC().Add(-relayDeadline - time.Second)}); err != nil {
		t.Fatal(err)
	}
	reply := answerRelay(folder, "allow")
	start := time.Now()
	if answer, err := RelayWait(folder, RelayRequest{}); err != nil || answer != "allow" {
		t.Fatalf("recovered Relay = %q, %v", answer, err)
	}
	if err := <-reply; err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= relayDeadline {
		t.Fatal("stale owner delayed the Relay until its deadline")
	}
	// A fresh lock of a live owner is not taken over.
	if err := writeRelayJSON(folder, "relay.lock", relayLock{PID: os.Getpid(), At: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if answer, err := RelayWait(folder, RelayRequest{}); err != nil || answer != "denied: no answer" {
		t.Fatalf("live owner = %q, %v", answer, err)
	}
}

func TestRelayTrackerDoesNotWakeForAnAnsweredRelay(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	client, callsPath := deferredSoloClient(t)
	link := &SoloLink{Client: client, ProjectID: 1, Orchestrator: 42}
	folder := t.TempDir()
	control := controlFolder(t, folder)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	task := &Task{Task: "abcd", Title: "Brief title", Folder: folder}
	rec := &Record{Path: folder, Task: "abcd"}
	tracker := &relayTracker{}
	request := RelayRequest{ID: "0123456789abcdef", ToolName: "Bash", ToolInput: json.RawMessage(`{}`), AskedAt: time.Now().UTC()}
	if err := writeRelayJSON(control, "relay-answer-"+request.ID+".json", relayAnswer{request, "allow", request.AskedAt.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	tracker.poll(&Env{Solo: link}, task, rec, "claude", request.AskedAt.Add(time.Second))
	if len(rec.Events) != 2 || len(rec.Permission.Relays) != 1 || tracker.open() {
		t.Fatalf("events: %+v", rec.Events)
	}
	if calls, err := os.ReadFile(callsPath); err == nil && strings.Contains(string(calls), "Maestro relay") {
		t.Fatalf("an answered Relay woke the Orchestrator:\n%s", calls)
	}
	// An open Relay still wakes it.
	open := RelayRequest{ID: "fedcba9876543210", ToolName: "Bash", ToolInput: json.RawMessage(`{}`), AskedAt: time.Now().UTC()}
	if err := writeRelayJSON(control, "relay.json", open); err != nil {
		t.Fatal(err)
	}
	tracker.poll(&Env{Solo: link}, task, rec, "claude", open.AskedAt)
	calls, err := os.ReadFile(callsPath)
	if err != nil || !strings.Contains(string(calls), "Maestro relay") {
		t.Fatalf("an open Relay did not wake the Orchestrator: %v\n%s", err, calls)
	}
}
