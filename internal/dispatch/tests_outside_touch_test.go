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
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestRecordTestsOutsideTouch(t *testing.T) {
	for _, tt := range []struct {
		name                        string
		touch                       []string
		deleted, flagged, violation bool
	}{
		{"edit outside touch", []string{"internal/hook/**"}, false, true, false},
		{"delete outside touch", []string{"internal/hook/**"}, true, false, true},
		{"edit inside touch", []string{"internal/other/**"}, false, false, false},
		{"delete inside touch", []string{"internal/other/**"}, true, false, false},
		{"negated test", []string{"internal/**", "!internal/other/**"}, false, true, false},
		{"no path limit", nil, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root, folder, path, snap := outsideTestBaseline(t)
			if tt.deleted {
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(root, path), []byte("package other\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			rec := &Record{Path: folder, Route: &router.Route{}, TaskInfo: TaskInfo{Access: brief.AccessEdit}, Outcome: Outcome{Outcome: OutcomeVerified}}
			recordChanges(&Task{Project: root, Access: brief.AccessEdit, Touch: tt.touch}, rec, snap)
			if !reflect.DeepEqual(rec.Verification.Changed, []string{path}) {
				t.Fatalf("changed: %v", rec.Verification.Changed)
			}
			if got := rec.Verification.TestsOutsideTouch; (len(got) > 0) != tt.flagged || (tt.flagged && !reflect.DeepEqual(got, []string{path})) {
				t.Fatalf("tests outside touch: %v", got)
			}
			if got := rec.Verification.TouchViolations; (len(got) > 0) != tt.violation || (tt.violation && !reflect.DeepEqual(got, []string{path})) {
				t.Fatalf("violations: %v", got)
			}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(folder, FileRecord))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), `"tests_outside_touch"`) != tt.flagged {
				t.Fatalf("record: %s", raw)
			}
			stored, err := LoadRecord(folder)
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.CreateTemp(t.TempDir(), "print")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			old := os.Stdout
			func() { os.Stdout = file; defer func() { os.Stdout = old }(); Print(stored, false) }()
			printed, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{string(printed), Status(stored)} {
				if strings.Contains(text, "tests outside touch: "+path) != tt.flagged {
					t.Fatalf("output: %s", text)
				}
				if tt.flagged && strings.Index(text, "changed") > strings.Index(text, "tests outside touch:") {
					t.Fatalf("output order: %s", text)
				}
			}
		})
	}
}

func TestGateIncludesTestsOutsideTouchDiff(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	root, folder, path, snap := outsideTestBaseline(t)
	if err := os.WriteFile(filepath.Join(root, path), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- body
		_, _ = io.WriteString(w, `{"answers":{}}`)
	}))
	defer server.Close()
	task := &Task{Project: root, Access: brief.AccessEdit, Touch: []string{"internal/hook/**"}}
	rec := &Record{Path: folder}
	recordChanges(task, rec, snap)
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}}
	gate := runGate(env, task, rec, &brief.Brief{Done: "Fix the hook"}, nil, snap)
	if gate.Skipped != "" {
		t.Fatal(gate.Skipped)
	}
	var payload struct {
		State struct {
			Tests struct {
				Note, Diff string
				Paths      []string
			} `json:"tests_outside_touch"`
		}
		Questions map[string]struct {
			Instructions string `json:"instructions"`
		}
	}
	if err := json.Unmarshal(<-requests, &payload); err != nil {
		t.Fatal(err)
	}
	tests := payload.State.Tests
	if tests.Note != "These test files are outside the Task's scope. A weakened or removed assertion that the Brief does not ask for is a Bad result." || !reflect.DeepEqual(tests.Paths, []string{path}) || !strings.Contains(tests.Diff, path) || !strings.Contains(tests.Diff, "-func TestAssertion") {
		t.Fatalf("gate tests: %+v", tests)
	}
	if payload.Questions["satisfies"].Instructions != "Does the diff satisfy every item under Done in the brief?" || payload.Questions["verdict"].Instructions != "Classify the diff against the brief." {
		t.Fatalf("gate questions: %+v", payload.Questions)
	}
}

func outsideTestBaseline(t *testing.T) (string, string, string, attemptBaseline) {
	t.Helper()
	root, folder, path := diffRepo(t), t.TempDir(), "internal/other/x_test.go"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte("package other\nfunc TestAssertion() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", path}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "Add test"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	return root, folder, path, snap
}
