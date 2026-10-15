package dispatch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/verify"
)

func TestGateRedactsBriefAndDiff(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- body
		_, _ = io.WriteString(w, `{"answers":{}}`)
	}))
	defer server.Close()
	root, folder := t.TempDir(), t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	path := filepath.Join(root, "changed.txt")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("MARKED\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}, Project: &config.Project{Redact: []string{"MARKED"}}}
	rec := &Record{Verification: Verification{Changed: []string{"changed.txt"}}}
	gate := runGate(env, &Task{Project: root, Title: "MARKED"}, rec, "MARKED", "MARKED", snap)
	if gate.Skipped != "" {
		t.Fatal(gate.Skipped)
	}
	body := <-requests
	if strings.Contains(string(body), "MARKED") {
		t.Fatalf("unredacted request: %s", body)
	}
	var payload struct {
		State struct {
			Brief struct{ Title, Goal, Done string }
			Diff  string
		}
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	b := payload.State.Brief
	if b.Title != "[redacted]" || b.Goal != "[redacted]" || b.Done != "[redacted]" || !strings.Contains(payload.State.Diff, "+[redacted]") {
		t.Fatalf("request: %s", body)
	}
}

func TestGateWithoutKeyIsSkipped(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "")
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev"}}}
	gate := runGate(env, &Task{}, &Record{}, "goal", "done", attemptBaseline{})
	if gate.Skipped == "" || gate.Satisfies != 0 || gate.Verdict != "" {
		t.Fatalf("missing key must skip verification: %+v", gate)
	}
}

func TestCollectDiffDoesNotRunConfiguredHelpers(t *testing.T) {
	for _, helper := range []string{"diff.external", "diff.test.textconv", "core.fsmonitor"} {
		t.Run(helper, func(t *testing.T) {
			root := diffRepo(t)
			marker := filepath.Join(t.TempDir(), "marker")
			t.Setenv("TEST_GIT_MARKER", marker)
			script := filepath.Join(t.TempDir(), "helper")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'called\\n' > \"$TEST_GIT_MARKER\"\nprintf 'helper output\\n'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			git := func(args ...string) {
				t.Helper()
				if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
			}
			git("config", helper, script)
			if helper == "diff.test.textconv" {
				if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("tracked.txt diff=test\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("after\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			start := verify.Head(root)
			if helper == "core.fsmonitor" {
				git("status", "--porcelain")
			} else {
				git("diff", "--ext-diff", "HEAD")
			}
			if err := os.Remove(marker); err != nil {
				t.Fatalf("configured helper did not run in the control: %v", err)
			}
			diff := collectDiff(root, start, []string{"tracked.txt"})
			if !strings.Contains(diff, "-before") || !strings.Contains(diff, "+after") {
				t.Fatalf("diff = %s", diff)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("configured helper ran during collectDiff: %v", err)
			}
		})
	}
}

func diffRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "Start")
	return root
}
