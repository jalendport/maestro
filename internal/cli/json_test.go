package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/ui"
)

func TestJSONCommandsPrintOneObject(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	project := t.TempDir()
	t.Chdir(project)
	folder := filepath.Join(project, ".agents", "maestro", "runs", "r-0001", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(controlFolder(t, folder), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(dispatch.RelayRequest{ID: "0123456789abcdef"})
	if err := os.WriteFile(filepath.Join(controlFolder(t, folder), "relay.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	oldQuiet := ui.Quiet
	t.Cleanup(func() { ui.Quiet = oldQuiet })
	for _, args := range [][]string{{"version", "--json"}, {"answer", "abcd", "deny", "--relay", "0123456789abcdef", "--json"}} {
		t.Run(args[0], func(t *testing.T) {
			root := (&app{version: "test"}).build()
			root.SetArgs(args)
			out := captureStdout(t, root.Execute)
			decoder := json.NewDecoder(strings.NewReader(out))
			var object map[string]any
			if err := decoder.Decode(&object); err != nil || len(object) == 0 {
				t.Fatalf("stdout = %q, error = %v", out, err)
			}
			if err := decoder.Decode(&object); err != io.EOF {
				t.Fatalf("extra stdout after result: %q, %v", out, err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(controlFolder(t, folder), "answer-deny")); err != nil {
		t.Fatalf("answer marker is not in the control folder: %v", err)
	}
}

func TestCouncilMergeExitMatchesCollectedResult(t *testing.T) {
	for _, single := range []bool{false, true} {
		a := &app{json: true}
		var mergeErr error
		out := captureStdout(t, func() error {
			mergeErr = a.printCouncilMerge(&council.Council{Slug: "test", SingleOpinion: single})
			return nil
		})
		if !json.Valid([]byte(out)) {
			t.Fatalf("invalid Merge: %q", out)
		}
		code, _ := proc.Code(mergeErr)
		if single && code != proc.CodeOrchestrator || !single && mergeErr != nil {
			t.Fatalf("SingleOpinion=%v, error=%v", single, mergeErr)
		}
	}
}

func TestHelpMatchesVerbFlagsAndPlanAccess(t *testing.T) {
	a := &app{}
	out := captureStdout(t, func() error { a.renderHelp(); return nil })
	for _, text := range []string{"cancel <task|run>", "status [run|task|council]", "hook [harness]", "adapter install [harness]", "(run, dispatch)"} {
		if !strings.Contains(out, text) {
			t.Fatalf("help is missing %q", text)
		}
	}
	if strings.Contains(out, "Not in this build yet") {
		t.Fatal("help says implemented verbs are absent")
	}
	if strings.Contains(planHelp, "- access: read") {
		t.Fatal("README edit example has read Access")
	}
}
