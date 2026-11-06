package dispatch_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/cli"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/run"
)

func TestRunTaskStatusAndReminderDuringChecks(t *testing.T) {
	for _, withPIDFile := range []bool{false, true} {
		for _, alive := range []bool{false, true} {
			t.Run(fmt.Sprintf("pid-file=%v/alive=%v", withPIDFile, alive), func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				root := t.TempDir()
				t.Chdir(root)
				folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
				if err := os.MkdirAll(folder, 0o700); err != nil {
					t.Fatal(err)
				}
				pid := 1 << 30
				if alive {
					pid = os.Getpid()
				}
				s := &run.State{Run: "r-0001", Project: root, Folder: filepath.Dir(folder), State: run.StateLive,
					Pid: pid, StartedAt: time.Now().Add(-130 * time.Second),
					Tasks: []*run.TaskState{{Number: 1, ID: "abcd", Title: "Check the Task", State: run.TaskRunning}}}
				if err := s.Save(); err != nil {
					t.Fatal(err)
				}
				if withPIDFile {
					if err := os.WriteFile(filepath.Join(s.Folder, "supervisor.pid"), []byte(fmt.Sprint(pid)), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				rec := &dispatch.Record{Task: "abcd", Run: s.Run, Project: root, Path: folder, State: "running",
					Pid: 1 << 30, StartedAt: s.StartedAt, Events: []dispatch.Event{{Kind: "check", Text: "running 1 check(s)"}}}
				if err := rec.Save(); err != nil {
					t.Fatal(err)
				}
				if got := dispatch.ProcessAlive(rec); got != alive {
					t.Fatalf("ProcessAlive = %v; want %v", got, alive)
				}
				out, code := statusOutput(t, s.Run)
				if code != 0 {
					t.Fatalf("status code %d: %s", code, out)
				}
				reminder := run.ReminderItems(root)
				if alive {
					for _, text := range []string{out, dispatch.Status(rec), reminder} {
						if !strings.Contains(text, "running") || !strings.Contains(text, "checks running") || strings.Contains(text, "stale") {
							t.Fatalf("checks phase missing: %s", text)
						}
					}
					if !strings.Contains(reminder, "running  task abcd") || !strings.Contains(reminder, "2m10s") {
						t.Fatalf("running Task missing: %s", reminder)
					}
				} else {
					for _, text := range []string{out, dispatch.Status(rec)} {
						if !strings.Contains(text, "stale (process gone)") || strings.Contains(text, "checks running") {
							t.Fatalf("stale Task missing: %s", text)
						}
					}
					if reminder != "" {
						t.Fatalf("dead Run in Reminder: %s", reminder)
					}
				}
			})
		}
	}
}

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
	if code != 0 || !strings.Contains(out, "dispatches\n  task abcd") || strings.Contains(out, "tab") {
		t.Fatalf("code %d: %s", code, out)
	}
	out, code = statusOutput(t, "abcd")
	if code != 0 || !strings.Contains(out, folder) || strings.Contains(out, "tab") {
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

func TestOldGateScoreInWaitAndStatus(t *testing.T) {
	for _, tc := range []struct {
		name, gate, line string
	}{
		{"old score", `{"satisfies":0.75,"verdict":"pass","risk":"low"}`, "gate     satisfies 0.75  verdict pass  risk low"},
		{"zero score", `{"scored":true,"satisfies":0,"verdict":"fail","risk":"high"}`, "gate     satisfies 0.00  verdict fail  risk high"},
		{"cause only", `{"cause":"brief","cause_source":"rule"}`, "gate     cause brief (rule)"},
		{"explicit false", `{"scored":false,"verdict":"pass","cause":"brief","cause_source":"rule"}`, "gate     cause brief (rule)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			root := t.TempDir()
			t.Chdir(root)
			folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			rec := &dispatch.Record{Task: "abcd", Project: root, Path: folder, State: "ended", EndedAt: time.Now(),
				Route: &router.Route{FamilyID: "codex-sol"}, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeVerified},
				Verification: dispatch.Verification{Gate: json.RawMessage(tc.gate)}}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"wait", "status"} {
				out, code := cliOutput(t, command, rec.Task)
				if code != 0 || !strings.Contains(out, tc.line) {
					t.Fatalf("%s = %d, %s; want %s", command, code, out, tc.line)
				}
				if strings.Contains(tc.line, "cause") && strings.Contains(out, "gate     satisfies") {
					t.Fatalf("%s shows a Cause as a score: %s", command, out)
				}
			}
		})
	}
}

func TestStatusShowsWorkdir(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &dispatch.Record{Task: "abcd", Project: root, Workdir: t.TempDir(), Path: folder, State: "ended", EndedAt: time.Now(),
		Route: &router.Route{FamilyID: "codex-sol"}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"abcd"}} {
		out, code := statusOutput(t, args...)
		if code != 0 || !strings.Contains(out, "workdir "+rec.Workdir+"\n") || strings.Index(out, "workdir ") < strings.Index(out, "codex-sol") {
			t.Fatalf("status code %d: %s", code, out)
		}
	}
	rec.Workdir = ""
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	out, code := statusOutput(t, "abcd")
	if code != 0 || strings.Contains(out, "workdir") {
		t.Fatalf("status without Worktree code %d: %s", code, out)
	}
}

func TestWaitDispatchHasNoTabText(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "")
	// A fresh discovery cache and an empty PATH keep wait from a refresh that starts real harnesses.
	t.Setenv("PATH", "")
	root := t.TempDir()
	t.Chdir(root)
	if err := (&discovery.Cache{RefreshedAt: time.Now()}).Save(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &dispatch.Record{Task: "abcd", Path: folder, State: "ended", Workdir: t.TempDir(),
		Route: &router.Route{FamilyID: "codex-sol"}, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeAnswered, Status: "done"}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	out, code := cliOutput(t, "wait", "abcd")
	if code != 0 || !strings.Contains(out, "task abcd") || !strings.Contains(out, "workdir "+rec.Workdir+"\n") || strings.Contains(out, "tab") {
		t.Fatalf("wait: code %d: %s", code, out)
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
