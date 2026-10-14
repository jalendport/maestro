package dispatch

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
				until := time.Now().Add(time.Second)
				for {
					if _, err := os.Stat(filepath.Join(folder, "relay.json")); err == nil {
						break
					}
					if time.Now().After(until) {
						t.Fatal("Relay did not open")
					}
					time.Sleep(time.Millisecond)
				}
				if err := os.WriteFile(filepath.Join(folder, "answer-"+answer), nil, 0o644); err != nil {
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
			b, err := os.ReadFile(filepath.Join(folder, "relay-answer.json"))
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
			if err := os.WriteFile(filepath.Join(folder, "answer-"+answer), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := RelayWait(folder, request)
			if err != nil || got != answer {
				t.Fatalf("answer: %q, %v; want %q", got, err, answer)
			}
			for _, name := range []string{"relay.json", "answer-allow", "answer-deny"} {
				if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
					t.Fatalf("%s remains: %v", name, err)
				}
			}
			var saved relayAnswer
			b, err := os.ReadFile(filepath.Join(folder, "relay-answer.json"))
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
	folder := t.TempDir()
	task := &Task{Task: "abcd", Folder: folder}
	rec := &Record{Path: folder, Task: "abcd"}
	env := &Env{}
	tracker := &relayTracker{}
	start := time.Now().UTC()
	for i, answer := range []string{"allow", "deny", "denied: no answer"} {
		request := RelayRequest{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"pwd"}`), AskedAt: start.Add(time.Duration(i) * 10 * time.Second)}
		if err := writeRelayJSON(folder, "relay.json", request); err != nil {
			t.Fatal(err)
		}
		tracker.poll(env, task, rec, "claude", request.AskedAt)
		tracker.poll(env, task, rec, "claude", request.AskedAt.Add(time.Second))
		if !tracker.open() || len(rec.Events) != i*2+1 {
			t.Fatalf("open Relay: %+v", rec.Events)
		}
		if err := writeRelayJSON(folder, "relay-answer.json", relayAnswer{request, answer, request.AskedAt.Add(5 * time.Second)}); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(folder, "relay.json")); err != nil {
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
	for _, fast := range []bool{false, true} {
		folder := t.TempDir()
		task := &Task{Task: "abcd", Folder: folder}
		rec := &Record{Path: folder, Task: "abcd"}
		tracker := &relayTracker{}
		env := &Env{}
		request := RelayRequest{ToolName: "Bash", ToolInput: json.RawMessage(`{}`), AskedAt: time.Now().UTC()}
		if fast {
			if err := writeRelayJSON(folder, "relay-answer.json", relayAnswer{request, "allow", request.AskedAt.Add(time.Second)}); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := writeRelayJSON(folder, "relay.json", request); err != nil {
				t.Fatal(err)
			}
			tracker.poll(env, task, rec, "claude", request.AskedAt)
			if err := os.Remove(filepath.Join(folder, "relay.json")); err != nil {
				t.Fatal(err)
			}
		}
		tracker.poll(env, task, rec, "claude", request.AskedAt.Add(time.Second))
		if len(rec.Events) != 2 || len(rec.Permission.Relays) != 1 || tracker.open() {
			t.Fatalf("events: %+v", rec.Events)
		}
	}
}
