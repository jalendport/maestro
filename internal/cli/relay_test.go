package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/dispatch"
)

func TestRelayCommandUsesEnvironmentAndProjectLookup(t *testing.T) {
	for _, useEnv := range []bool{false, true} {
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
		if err := os.WriteFile(filepath.Join(folder, "answer-allow"), nil, 0o644); err != nil {
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
