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
	"strconv"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

func TestDispatchRoots(t *testing.T) {
	for _, worktree := range []bool{false, true} {
		t.Run(map[bool]string{false: "Project", true: "Worktree"}[worktree], func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("HOME", t.TempDir())
			t.Setenv("GIT_AUTHOR_NAME", "Test")
			t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
			t.Setenv("GIT_COMMITTER_NAME", "Test")
			t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("TYPESAFE_API_KEY", "test-key")
			project := t.TempDir()
			git := func(root string, args ...string) {
				t.Helper()
				if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			write := func(path, content string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			git(project, "init", "-q")
			write(filepath.Join(project, ".gitignore"), ".agents/\n")
			write(filepath.Join(project, "tracked.txt"), "project base\n")
			git(project, "add", ".")
			git(project, "-c", "commit.gpgsign=false", "commit", "-qm", "Start")
			root, workdir, before := project, "", "project base"
			if worktree {
				root = filepath.Join(t.TempDir(), "worktree")
				workdir, before = root, "worktree base"
				git(project, "worktree", "add", "-b", "task", root, "HEAD")
				write(filepath.Join(root, "tracked.txt"), before+"\n")
				git(root, "add", ".")
				git(root, "-c", "commit.gpgsign=false", "commit", "-qm", "Worktree base")
				write(filepath.Join(project, "tracked.txt"), "project dirty\n")
			}
			write(filepath.Join(project, "other.txt"), "other Task\n")
			folder := filepath.Join(config.RunsDir(project), "r-test", "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(t.TempDir(), "lane")
			script := "#!/bin/sh\ncat > \"$MAESTRO_TASK_FOLDER/pointer\"\npwd > \"$MAESTRO_TASK_FOLDER/cwd\"\nprintf '%s\\n' \"$@\" > \"$MAESTRO_TASK_FOLDER/args\"\nif [ \"$TEST_ATTEMPT\" = 1 ]; then\nprintf 'Lane change\\n' > tracked.txt\nfi\nprintf 'STATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
			if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var payload struct {
					State struct{ Diff string }
				}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Error(err)
				}
				requests <- payload.State.Diff
				_, _ = io.WriteString(w, `{"answers":{"satisfies":{"noul":1},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}},"cause":{"choice":"none"}}}`)
			}))
			defer server.Close()
			env := &Env{Root: project, Reg: harness.New(map[string]string{"codex": bin}),
				Roster:  &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}},
				Project: &config.Project{Checks: []string{"pwd"}, Allow: []string{"Bash(project-rule)"}}}
			task := &Task{Project: project, Workdir: workdir, Folder: folder, Run: "r-test", Task: "abcd",
				Access: brief.AccessEdit, Touch: []string{"tracked.txt"}, Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
			if err := task.Save(); err != nil {
				t.Fatal(err)
			}
			task, err := LoadTask(folder)
			if err != nil || task.Workdir != workdir {
				t.Fatalf("Task Workdir: %+v, %v", task, err)
			}
			start := verify.Head(root)
			for attempt := 1; attempt <= 2; attempt++ {
				t.Setenv("TEST_ATTEMPT", strconv.Itoa(attempt))
				rec := runOnce(env, task, &brief.Brief{Title: "Use the two roots", Done: "Change tracked.txt"}, attempt, nil)
				if rec.Outcome.Outcome != OutcomeVerified || rec.Project != project || rec.Workdir != workdir || rec.Path != folder {
					t.Fatalf("Record: %+v", rec)
				}
				if rec.Verification.StartCommit != start || !reflect.DeepEqual(rec.Verification.Changed, []string{"tracked.txt"}) || len(rec.Verification.TouchViolations) != 0 {
					t.Fatalf("Verification: %+v", rec.Verification)
				}
				if checks := rec.Verification.Checks; len(checks) != 1 || !checks[0].OK || strings.TrimSpace(checks[0].Output) != root {
					t.Fatalf("checks: %+v", checks)
				}
				select {
				case diff := <-requests:
					if !strings.Contains(diff, "-"+before) || !strings.Contains(diff, "+Lane change") || strings.Contains(diff, "project dirty") {
						t.Fatalf("gate diff: %s", diff)
					}
				default:
					t.Fatal("gate did not receive the diff")
				}
				stored, err := LoadRecord(folder)
				if err != nil || stored.Workdir != workdir || !strings.Contains(DiffFor(project, stored), "+Lane change") {
					t.Fatalf("stored Record: %+v, %v", stored, err)
				}
				read := func(name string) string {
					t.Helper()
					data, err := os.ReadFile(filepath.Join(folder, name))
					if err != nil {
						t.Fatal(err)
					}
					return string(data)
				}
				cwd, cwdErr := filepath.EvalSymlinks(strings.TrimSpace(read("cwd")))
				wantRoot, rootErr := filepath.EvalSymlinks(root)
				if cwdErr != nil || rootErr != nil || cwd != wantRoot {
					t.Fatalf("Lane Root = %s (%v), want %s (%v)", cwd, cwdErr, wantRoot, rootErr)
				}
				if !strings.Contains(read("args"), "-C\n"+root+"\n") || !strings.Contains(read("args"), "--add-dir\n"+folder+"\n") {
					t.Fatalf("launch args: %s", read("args"))
				}
				pointer := brief.Pointer(filepath.Join(folder, FileBrief))
				if !worktree {
					rel, _ := filepath.Rel(project, filepath.Join(folder, FileBrief))
					pointer = brief.Pointer(rel)
				}
				if strings.TrimSpace(read("pointer")) != pointer {
					t.Fatalf("Pointer: %s", read("pointer"))
				}
				if !strings.Contains(read(FileBrief), "`pwd`") {
					t.Fatalf("project checks missing from Brief: %s", read(FileBrief))
				}
				control, err := config.ControlDirForFolder(folder)
				if err != nil {
					t.Fatal(err)
				}
				lane, ok := hook.LoadLane(control)
				if !ok || lane.Root != root || lane.RunFolder != folder || !reflect.DeepEqual(lane.Allow, env.Project.Allow) {
					t.Fatalf("Lane policy: %+v", lane)
				}
				var snap attemptBaseline
				if err := json.Unmarshal([]byte(read(FileBaseline)), &snap); err != nil {
					t.Fatal(err)
				}
				if attempt == 1 {
					if worktree && len(snap.Dirty) != 0 {
						t.Fatalf("Worktree baseline is not clean: %+v", snap)
					}
					archiveAttempt(folder, attempt)
				} else {
					write(filepath.Join(root, "outside.txt"), "outside Touch\n")
					recordChanges(task, rec, snap)
					if !reflect.DeepEqual(rec.Verification.TouchViolations, []string{"outside.txt"}) {
						t.Fatalf("Touch guard: %+v", rec.Verification)
					}
				}
			}
		})
	}
}
