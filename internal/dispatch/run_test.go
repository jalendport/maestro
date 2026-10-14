package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

func TestRunOnceTakesFreshBaseline(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	folder := t.TempDir()
	old, err := verify.Take(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.SaveTo(filepath.Join(folder, FileBaseline)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "earlier-task.txt"), []byte("earlier work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\ncat >/dev/null\nprintf 'new work\\n' > \"$TEST_CHANGED\"\nprintf 'STATUS: done\\n' > \"$TEST_REPORT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &roster.Roster{}
	r.Router.Backend = "rules"
	env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessEdit,
		Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
	for i, path := range []string{"first.txt", "retry.txt", "escalation.txt"} {
		task.Touch = []string{path}
		t.Setenv("TEST_CHANGED", filepath.Join(root, path))
		rec := runOnce(env, task, &brief.Brief{Title: "Test attempt"}, i+1, nil)
		if rec.Outcome.Outcome != OutcomeUnverified {
			t.Fatalf("attempt %d: outcome %+v", i+1, rec.Outcome)
		}
		if got := rec.Verification.Changed; !reflect.DeepEqual(got, []string{path}) {
			t.Fatalf("attempt %d: changed %v, want only %s", i+1, got, path)
		}
		snap, err := verify.LoadSnapshot(filepath.Join(folder, FileBaseline))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := snap.Dirty["earlier-task.txt"]; !ok || !snap.Taken.After(old.Taken) {
			t.Fatalf("attempt %d did not replace the old baseline: %+v", i+1, snap)
		}
		old = snap
	}
}

func TestRunOncePausesLimitsAndRecordsRelay(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\ncat >/dev/null\nsleep 5\nprintf '## Answer\\nanswer\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: &roster.Roster{}, Project: &config.Project{Allow: []string{"Bash(go run *)"}}, Reg: harness.New(map[string]string{"claude": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessRead, Route: &router.Route{Harness: "claude"}, WallLimit: "3s", IdleLimit: "3s"}
	done := make(chan error, 1)
	go func() {
		until := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(folder, FileRecord)); err == nil {
				break
			}
			if time.Now().After(until) {
				done <- fmt.Errorf("Lane did not start")
				return
			}
			time.Sleep(time.Millisecond)
		}
		request := RelayRequest{ToolName: "Bash", ToolInput: []byte(`{"command":"pwd"}`), AskedAt: time.Now().UTC()}
		if err := writeRelayJSON(folder, "relay.json", request); err != nil {
			done <- err
			return
		}
		time.Sleep(4 * time.Second)
		if err := writeRelayJSON(folder, "relay-answer.json", relayAnswer{request, "deny", time.Now().UTC()}); err != nil {
			done <- err
			return
		}
		done <- os.Remove(filepath.Join(folder, "relay.json"))
	}()
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rec.Outcome.Timer != "" || rec.Outcome.Outcome != OutcomeAnswered {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	var kinds []string
	for _, event := range rec.Events {
		if strings.HasPrefix(event.Kind, "relay") {
			kinds = append(kinds, event.Kind)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"relay", "relay-answer"}) || len(rec.Permission.Relays) != 1 {
		t.Fatalf("Relay events: %v, records: %s", kinds, rec.Permission.Relays)
	}
}

func TestArchiveAttemptPreservesBaselineTabLogAndLane(t *testing.T) {
	folder := t.TempDir()
	for _, name := range []string{FileBaseline, "tab.log", "lane.json"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archiveAttempt(folder, 1)
	for _, name := range []string{FileBaseline, "tab.log", "lane.json"} {
		b, err := os.ReadFile(filepath.Join(folder, FileAttempts, "1", name))
		if err != nil || string(b) != name {
			t.Fatalf("archive %s: %q, %v", name, b, err)
		}
		if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("archive left %s in the Task folder: %v", name, err)
		}
	}
}

func TestClearAnswersRemovesLane(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "lane.json")
	if err := os.WriteFile(path, []byte(`{"access":"edit"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	clearAnswers(folder)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Lane permissions remain after cleanup: %v", err)
	}
}

func TestRunOnceWritesLaneForEveryHarnessAndAccess(t *testing.T) {
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		for _, access := range []string{brief.AccessRead, brief.AccessEdit} {
			t.Run(name+"/"+access, func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				root, folder := t.TempDir(), t.TempDir()
				if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, out)
				}
				bin := filepath.Join(t.TempDir(), "lane")
				script := "#!/bin/sh\ncat >/dev/null\n[ -s \"$MAESTRO_TASK_FOLDER/lane.json\" ] || exit 7\nprintf '## Answer\\nfile report\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
				if name == "agy" {
					script += "printf '%s\\n' '{\"status\":\"SUCCESS\",\"response\":\"This final message is not a Report\",\"usage\":{\"input_tokens\":12,\"output_tokens\":4}}'\n"
				}
				if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				project := &config.Project{Allow: []string{"Bash(go run *)"}, Checks: []string{"git status --short"}}
				env := &Env{Root: root, Roster: &roster.Roster{}, Project: project, Reg: harness.New(map[string]string{name: bin})}
				task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: access,
					Dirs: []string{t.TempDir()}, Checks: []string{"git diff --name-only"}, Commit: access == brief.AccessEdit,
					Route: &router.Route{Harness: name}, WallLimit: "1m", IdleLimit: "1m"}
				for attempt := 1; attempt <= 2; attempt++ {
					want := &hook.Lane{Task: task.Task, Harness: name, Access: access, Root: root, Dirs: task.Dirs,
						RunFolder: folder, Allow: []string{}, Checks: []string{}, Commit: task.Commit}
					if access == brief.AccessEdit {
						want.Allow = project.Allow
						want.Checks = []string{"git status --short", "git diff --name-only"}
					}
					rec := runOnce(env, task, &brief.Brief{}, attempt, nil)
					if rec.Outcome.Outcome != OutcomeAnswered && rec.Outcome.Outcome != OutcomeUnverified {
						t.Fatalf("outcome: %+v", rec.Outcome)
					}
					got, ok := hook.LoadLane(folder)
					if !ok || !reflect.DeepEqual(got, want) {
						t.Fatalf("Lane permissions: %+v, want %+v", got, want)
					}
					if name == "agy" {
						report, ok := rec.Report()
						if !ok || !strings.Contains(report.Raw, "file report") || rec.Outcome.ReportByText || rec.Budget.Tokens.Input == nil || *rec.Budget.Tokens.Input != 12 {
							t.Fatalf("Antigravity Report and tokens: %+v", rec)
						}
					}
					task.Dirs = []string{t.TempDir()}
					project.Allow = []string{"Bash(go vet *)"}
				}
			})
		}
	}
}

func TestRunOnceAgyUsesSoloTab(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	t.Setenv("MAESTRO_TEST_REPORT", filepath.Join(folder, FileReport))
	client, callsPath := deferredSoloClient(t)
	env := &Env{Root: root, Roster: &roster.Roster{Harness: map[string]config.Harness{"agy": {SoloTool: 9}}},
		Reg: harness.New(map[string]string{"agy": "/must-not-run-one-shot"}), Solo: &SoloLink{Client: client, ProjectID: 1}}
	task := &Task{Project: root, Folder: folder, Run: "council", Task: "seat", Access: brief.AccessRead, Council: true,
		Route: &router.Route{Harness: "agy", Model: "gemini-3.8-flash", Effort: "high"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{Title: "Council"}, 1, nil)
	if rec.SoloProcess != 42 || rec.Outcome.Outcome != OutcomeAnswered || rec.Outcome.ReportByText {
		t.Fatalf("Antigravity tab outcome: %+v", rec.Outcome)
	}
	lane, ok := hook.LoadLane(folder)
	if !ok || !lane.Tab || lane.Harness != "agy" || lane.Access != brief.AccessRead {
		t.Fatalf("tab permissions: %+v", lane)
	}
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	var spawn struct {
		Params struct {
			Name      string `json:"name"`
			Arguments struct {
				ExtraArgs []string `json:"extra_args"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(calls), "\n")[0]), &spawn); err != nil {
		t.Fatal(err)
	}
	want := []string{"--sandbox", "--model", "gemini-3.8-flash", "--effort", "high", "--prompt-interactive", brief.Pointer(filepath.Join(folder, FileBrief))}
	if spawn.Params.Name != "spawn_agent" || !reflect.DeepEqual(spawn.Params.Arguments.ExtraArgs, want) {
		t.Fatalf("tab launch: %+v, want %q", spawn, want)
	}
}

func TestAttemptDiffUsesDirtyFileContentsAtStart(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	path := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(path, []byte("earlier Task\nold line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	snap, err := takeBaseline(root, folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("earlier Task\nnew line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := snap.diff(root, []string{"shared.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-old line") || !strings.Contains(diff, "+new line") || strings.Contains(diff, "+earlier Task") {
		t.Fatalf("diff includes work before the attempt: %s", diff)
	}
	rec := &Record{Path: folder, Verification: Verification{Changed: []string{"shared.txt"}}}
	if got := DiffFor(root, rec); got != diff {
		t.Fatalf("saved baseline diff = %q, want %q", got, diff)
	}
}

func TestEditPreflightRequiresGitBeforeRouting(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			if missing {
				t.Setenv("PATH", t.TempDir())
			}
			env := &Env{Root: t.TempDir(), Roster: &roster.Roster{}}
			b := &brief.Brief{Access: brief.AccessEdit}
			if _, err := Prepare(env, b, router.Request{}, "", ""); err == nil || !strings.Contains(err.Error(), "snapshot needs git") {
				t.Fatalf("preflight error: %v", err)
			}
		})
	}
}

func TestReadDispatchWorksWithoutGitOrRepository(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\nprintf '## Answer\\nanswer\\nSTATUS: done\\n' > \"$TEST_REPORT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_REPORT", filepath.Join(folder, FileReport))
	env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"codex": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "read", Access: brief.AccessRead, Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.Outcome != OutcomeAnswered {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
}

func TestUnknownFullClaudeModelKeepsCLIError(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, folder := t.TempDir(), t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	message := "There is an issue with the selected model (claude-opuss-5-5). It may not exist or you may not have access to it."
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '" + message + "' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"claude": bin})}
	task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "bad-model", Access: brief.AccessRead,
		Route: &router.Route{Harness: "claude", Model: "claude-opuss-5-5"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.FailureClass != FailLane || rec.Outcome.ErrorText != message {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
	stored, err := LoadRecord(folder)
	if err != nil || stored.Outcome.FailureClass != FailLane || stored.Outcome.ErrorText != message {
		t.Fatalf("stored record=%+v err=%v", stored, err)
	}
}
