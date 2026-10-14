package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
)

func TestRelayCommandUsesEnvironmentAndProjectLookup(t *testing.T) {
	for _, useEnv := range []bool{false, true} {
		t.Setenv("MAESTRO_HOME", t.TempDir())
		root := t.TempDir()
		folder := filepath.Join(root, ".agents", "maestro", "runs", "r-test", "abcd")
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(cwd) })
		t.Setenv("MAESTRO_TASK_FOLDER", "")
		id := "abcd"
		if useEnv {
			t.Setenv("MAESTRO_TASK_FOLDER", folder)
			id = "not-found"
		}
		control := config.ControlDirForFolder(folder)
		if err := os.MkdirAll(control, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(control, "answer-allow"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := (&app{}).relayCmd()
		if !cmd.Hidden {
			t.Fatal("relay is visible")
		}
		cmd.SetIn(strings.NewReader(`{"tool_name":"Bash","tool_input":{}}`))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{id})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"behavior":"allow"`) {
			t.Fatalf("output: %s", out.String())
		}
	}
}

func TestWaitRelayEventsPrintOnce(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })
	rec := &dispatch.Record{Task: "abcd", Events: []dispatch.Event{{Kind: "relay", Text: "Bash pwd"}, {Kind: "relay-answer", Text: "deny"}}}
	seen := map[string]bool{}
	printRelayEvents(rec, seen)
	printRelayEvents(rec, seen)
	w.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if strings.Count(out.String(), "maestro answer abcd allow|deny") != 1 || strings.Count(out.String(), "relay-answer") != 1 {
		t.Fatalf("output: %s", out.String())
	}
}

func TestCleanRemovesRunAndCouncilControlFolders(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	for _, tc := range []struct{ parent, run, task string }{
		{config.RunsDir(root), "r-0001", "abcd"},
		{config.CouncilsDir(root), "review", "agy-flash"},
	} {
		folder := filepath.Join(tc.parent, tc.run, tc.task)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		control := config.ControlDir(tc.run, tc.task)
		if err := os.MkdirAll(control, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"read"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(filepath.Dir(folder), old, old); err != nil {
			t.Fatal(err)
		}
	}
	seat := filepath.Join(config.CouncilsDir(root), "review", "round-2", "agy-flash")
	if err := os.MkdirAll(seat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seat, dispatch.FileTask), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	control := config.ControlDirForFolder(seat)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"read"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(config.CouncilsDir(root), "review"), old, old); err != nil {
		t.Fatal(err)
	}
	command := (&app{}).cleanCmd()
	command.SetArgs([]string{"--older-than", "1d"})
	captureStdout(t, command.Execute)
	for _, folder := range []string{filepath.Join(config.RunsDir(root), "r-0001"), filepath.Join(config.CouncilsDir(root), "review"), config.ControlDir("r-0001", ""), config.ControlDir("review", ""), control} {
		if _, err := os.Stat(folder); !os.IsNotExist(err) {
			t.Fatalf("clean left %s: %v", folder, err)
		}
	}
}
