package dispatch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestCommittedAttemptReachesScopeGuardAndGateDiff(t *testing.T) {
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
			root, folder := t.TempDir(), t.TempDir()
			env := &Env{Roster: &roster.Roster{}}
			rec := &Record{Path: folder}
			task := &Task{Project: root, Access: brief.AccessEdit}
			judge(env, task, rec, brief.ParseReport("STATUS: done\n"), []string{command}, &brief.Brief{}, attemptBaseline{})
			want := "check failed: " + command + ": " + firstLine(rec.Verification.Checks[0].Output)
			if rec.Outcome.ErrorText != want || rec.Outcome.Outcome != OutcomeFailed {
				t.Fatalf("outcome: %+v, want %q", rec.Outcome, want)
			}
		})
	}
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
	if !strings.Contains(firstLine(string(printed)), "no checks configured") {
		t.Fatalf("print: %s", printed)
	}
}

func TestRunOnceDrainsFinalStreamBeforeWait(t *testing.T) {
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
	task := &Task{Project: root, Folder: folder, Task: "abcd", Access: brief.AccessRead, Route: &router.Route{Harness: "claude"}, WallLimit: "1m", IdleLimit: "1m"}
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
