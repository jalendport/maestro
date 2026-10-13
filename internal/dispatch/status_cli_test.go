package dispatch_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/cli"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/run"
)

func statusOutput(t *testing.T, args ...string) (string, int) {
	t.Helper()
	return cliOutput(t, append([]string{"status"}, args...)...)
}

func cliOutput(t *testing.T, args ...string) (string, int) {
	t.Helper()
	oldArgs, oldOut := os.Args, os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	os.Args = append([]string{"maestro"}, args...)
	os.Stdout = w
	defer func() { os.Args, os.Stdout = oldArgs, oldOut }()
	code := cli.Execute("test")
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestStatusWithOnlyStandaloneDispatch(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&dispatch.Record{Task: "abcd", Path: folder, State: "running", StartedAt: time.Now()}).Save(); err != nil {
		t.Fatal(err)
	}
	out, code := statusOutput(t)
	if code != 0 || !strings.Contains(out, "dispatches\n  task abcd") {
		t.Fatalf("code %d: %s", code, out)
	}
	out, code = statusOutput(t, "abcd")
	if code != 0 || !strings.Contains(out, folder) {
		t.Fatalf("task query: code %d: %s", code, out)
	}
	out, code = statusOutput(t, "--json")
	var status struct {
		Runs       []json.RawMessage `json:"runs"`
		Dispatches []dispatch.Record `json:"dispatches"`
		Councils   []json.RawMessage `json:"councils"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}
	if code != 0 || len(status.Runs) != 0 || len(status.Dispatches) != 1 || status.Councils == nil {
		t.Fatalf("JSON status: %s", out)
	}
}

func TestStatusRunOrderAndCouncilQuery(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	for _, id := range []string{"r-0001", "r-0002", "r-0003"} {
		folder := filepath.Join(config.RunsDir(root), id)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		s := &run.State{Run: id, Project: root, Folder: folder, State: run.StateLive, StartedAt: time.Now()}
		if id == "r-0003" {
			s.State = run.StateFinished
		}
		if err := s.Save(); err != nil {
			t.Fatal(err)
		}
		if id != "r-0003" {
			if err := run.WriteIndex(s); err != nil {
				t.Fatal(err)
			}
		}
	}
	folder := filepath.Join(config.CouncilsDir(root), "review")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&council.Council{Slug: "review", Path: folder, State: "running", Pid: os.Getpid(), Tier: roster.TierDaily}).Save(); err != nil {
		t.Fatal(err)
	}
	out, code := statusOutput(t)
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	indices := []int{strings.Index(out, "run r-0003"), strings.Index(out, "run r-0002"), strings.Index(out, "run r-0001"), strings.Index(out, "councils")}
	for i, index := range indices {
		if index < 0 || (i > 0 && index <= indices[i-1]) {
			t.Fatalf("section order: %s", out)
		}
	}
	out, code = statusOutput(t, "review")
	if code != 0 || !strings.Contains(out, "council review  Daily  round 1") || !strings.Contains(out, folder) {
		t.Fatalf("Council query: code %d: %s", code, out)
	}
	out, code = statusOutput(t, "r-0001")
	if code != 0 || !strings.HasPrefix(out, "run r-0001") || strings.Contains(out, "councils") {
		t.Fatalf("Run query: code %d: %s", code, out)
	}
}
