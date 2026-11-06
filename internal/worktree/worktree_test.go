package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/verify"
)

func repository(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "user@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "user@example.test")
	t.Setenv("GIT_EDITOR", "true")
	root := t.TempDir()
	tgit(t, root, "init", "-b", "master")
	tgit(t, root, "config", "user.name", "Test User")
	tgit(t, root, "config", "user.email", "user@example.test")
	write(t, root, "base.txt", "base\n")
	write(t, root, "delete.txt", "delete\n")
	tgit(t, root, "add", ".")
	tgit(t, root, "commit", "-m", "Start test repository")
	return root
}

func tgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(dir, "", args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openRun(t *testing.T, root string) Run {
	t.Helper()
	r, err := Open(root, "r-0078")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := Remove(root, r.ID); err != nil {
			t.Error(err)
		}
	})
	return r
}

func addTask(t *testing.T, r Run, task string) Worktree {
	t.Helper()
	w, err := r.Add(task)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func commitTask(t *testing.T, w Worktree, name, content string) string {
	t.Helper()
	write(t, w.Dir, name, content)
	hash, err := w.Commit("Change test file\n\nCo-Authored-By: "+Identity("codex-sol", "gpt-6.1-sol"), []string{name})
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func mergeTask(t *testing.T, r Run, task string) MergeResult {
	t.Helper()
	result, err := r.Merge(task)
	if err != nil || len(result.Conflicts) != 0 {
		t.Fatalf("Merge: %+v, %v", result, err)
	}
	return result
}

func TestOpenAndAddResume(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	base := tgit(t, root, "rev-parse", "HEAD")
	if r.Base != base || r.Branch != "maestro/r-0078/run" {
		t.Fatalf("Run: %+v", r)
	}
	w := addTask(t, r, "5280")
	if got := tgit(t, w.Dir, "symbolic-ref", "--short", "HEAD"); got != "maestro/r-0078/5280" {
		t.Fatalf("Task branch: %s", got)
	}
	control, err := config.ControlDirForFolder(filepath.Join(root, ".agents", "maestro", "runs", r.ID, "5280"))
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Base(filepath.Dir(filepath.Dir(control)))
	if want := filepath.Join(config.Home(), "worktrees", key, r.ID, "5280"); w.Dir != want {
		t.Fatalf("Worktree: %s, want %s", w.Dir, want)
	}
	write(t, w.Dir, "pending.txt", "keep\n")
	resumed := addTask(t, r, "5280")
	if resumed != w {
		t.Fatalf("resumed Worktree: %+v", resumed)
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "pending.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(w.Dir, "pending.txt")); err != nil {
		t.Fatal(err)
	}
	hash := commitTask(t, w, "first.txt", "first\n")
	mergeTask(t, r, "5280")
	again, err := Open(root, r.ID)
	if err != nil || again.Base != base {
		t.Fatalf("Open again: %+v, %v", again, err)
	}
	if got := tgit(t, root, "rev-parse", r.Branch); got != hash {
		t.Fatalf("Run head: %s, want %s", got, hash)
	}
	next := addTask(t, again, "5281")
	if got := tgit(t, next.Dir, "rev-parse", "HEAD"); got != hash {
		t.Fatalf("next Task base: %s, want %s", got, hash)
	}
}

func TestExistingIDPrefersNewIdentity(t *testing.T) {
	for _, mode := range []string{"none", "old folder", "old branch", "new folder", "new branch"} {
		t.Run(mode, func(t *testing.T) {
			root := repository(t)
			preferred, old := "d-r-0001-abcd", "abcd"
			if mode != "none" && mode != "old folder" {
				tgit(t, root, "branch", "maestro/"+old+"/run")
			}
			if mode == "old folder" || mode == "new folder" {
				id := old
				if mode == "new folder" {
					id = preferred
				}
				dir, err := runDir(root, id)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "new branch" {
				tgit(t, root, "branch", "maestro/"+preferred+"/abcd")
			}
			want := preferred
			if strings.HasPrefix(mode, "old ") {
				want = old
			}
			got, err := ExistingID(root, preferred, old)
			if err != nil || got != want {
				t.Fatalf("identity: %s, want %s: %v", got, want, err)
			}
		})
	}
}

func TestDirty(t *testing.T) {
	root := repository(t)
	write(t, root, ".gitignore", "ignored/\n")
	tgit(t, root, "add", ".gitignore")
	tgit(t, root, "commit", "-m", "Ignore test directory")
	write(t, root, "ignored/file", "ignored")
	paths, err := Dirty(root)
	if err != nil || len(paths) != 0 {
		t.Fatalf("clean: %v, %v", paths, err)
	}
	write(t, root, "base.txt", "changed\n")
	write(t, root, "new file\n.txt", "new\n")
	tgit(t, root, "mv", "delete.txt", "renamed.txt")
	paths, err = Dirty(root)
	want := []string{"base.txt", "delete.txt", "new file\n.txt", "renamed.txt"}
	if err != nil || !reflect.DeepEqual(paths, want) {
		t.Fatalf("Dirty: %q, %v; want %q", paths, err, want)
	}
}

func TestLink(t *testing.T) {
	root := repository(t)
	write(t, root, ".gitignore", "node_modules/\nvendor/\nstorage/framework/\n")
	tgit(t, root, "add", ".gitignore")
	tgit(t, root, "commit", "-m", "Ignore dependencies")
	write(t, root, "node_modules/package/index.js", "dependency")
	write(t, root, "vendor/package/file", "dependency")
	write(t, root, "storage/framework/cache/file", "dependency")
	write(t, root, ".venv/file", "not ignored")
	dir := t.TempDir()
	tgit(t, dir, "init", "-b", "master")
	linked, err := Link(root, dir, []string{"vendor/**", "storage/framework/cache/**"})
	if err != nil || !reflect.DeepEqual(linked.Linked, []string{"node_modules"}) || !reflect.DeepEqual(linked.Refused, []string{"vendor", "storage/framework"}) {
		t.Fatalf("Link: %v, %v", linked, err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "node_modules")); err != nil || target != filepath.Join(root, "node_modules") {
		t.Fatalf("symlink: %s, %v", target, err)
	}
	for _, name := range []string{"vendor", "storage/framework", ".venv"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected link %s: %v", name, err)
		}
	}
	preview := t.TempDir()
	if _, err := Link(root, preview); err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(preview, "node_modules")); err != nil {
		t.Fatalf("preview link: %v", err)
	}
	for _, pattern := range []string{"node_modules/**", "./node_modules/**", "node_modules/pkg/**", "/node_modules/", "node_modules"} {
		linked, err := Link(root, "", []string{pattern})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(linked.Refused, []string{"node_modules"}) {
			t.Errorf("pattern %q: refused %v", pattern, linked.Refused)
		}
	}
	for _, touch := range [][]string{nil, {}, {"**"}, {"**/index.js"}, {"index.js"}, {"[!v]ode_modules/**"}, {"node_*"}, {"!node_modules/**"}, {"src/**"}} {
		linked, err := Link(root, "", touch)
		if err != nil || len(linked.Refused) != 0 {
			t.Fatalf("touch %v: %+v, %v", touch, linked, err)
		}
	}
	r := openRun(t, root)
	w, err := r.Add("5280", []string{"node_modules/**"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(w.Dir, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("Add linked writable dependency: %v", err)
	}
	if _, err := os.Readlink(filepath.Join(w.Dir, "vendor")); err != nil {
		t.Fatal(err)
	}
}

func TestCommitPathsIdentityAndTrailer(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "5280")
	write(t, root, "user-staged.txt", "keep user index\n")
	tgit(t, root, "add", "user-staged.txt")
	before := tgit(t, root, "ls-files", "--stage")
	write(t, w.Dir, "keep-staged.txt", "leave staged\n")
	tgit(t, w.Dir, "add", "keep-staged.txt")
	write(t, w.Dir, "base.txt", "changed\n")
	write(t, w.Dir, "new.txt", "new\n")
	write(t, w.Dir, "unstaged.txt", "leave unstaged\n")
	if err := os.Remove(filepath.Join(w.Dir, "delete.txt")); err != nil {
		t.Fatal(err)
	}
	trailer := "Co-Authored-By: " + Identity("codex-sol", "gpt-6.1-sol")
	hash, err := w.Commit("Change selected files\n\n"+trailer, []string{"base.txt", "delete.txt", "new.txt"})
	if err != nil || len(hash) != 40 {
		t.Fatalf("Commit: %s, %v", hash, err)
	}
	if got := tgit(t, w.Dir, "show", "--format=", "--name-only", "HEAD"); got != "base.txt\ndelete.txt\nnew.txt" {
		t.Fatalf("committed paths: %s", got)
	}
	if got := tgit(t, w.Dir, "show", "-s", "--format=%an <%ae>%n%cn <%ce>%n%B", "HEAD"); got != "Test User <user@example.test>\nTest User <user@example.test>\nChange selected files\n\n"+trailer {
		t.Fatalf("identity and message: %s", got)
	}
	if got := tgit(t, w.Dir, "diff", "--cached", "--name-only"); got != "keep-staged.txt" {
		t.Fatalf("Task index: %s", got)
	}
	if got := tgit(t, root, "ls-files", "--stage"); got != before {
		t.Fatalf("user index changed: %s", got)
	}
	if got, err := w.Commit("No changes", nil); got != "" || err != nil {
		t.Fatalf("empty Commit: %s, %v", got, err)
	}
	if got := tgit(t, w.Dir, "rev-parse", "HEAD"); got != hash {
		t.Fatalf("empty Commit moved HEAD: %s", got)
	}
}

func TestIdentity(t *testing.T) {
	for _, tc := range []struct{ family, model, want string }{
		{"codex-sol", "gpt-6.1-sol", "Codex <noreply@openai.com>"},
		{"claude-opus", "claude-opus-4-8", "Claude Opus 4.8 <noreply@anthropic.com>"},
		{"claude-sonnet", "claude-sonnet-4-5-20250929", "Claude Sonnet 4.5 <noreply@anthropic.com>"},
		{"claude-other", "unknown", "Claude <noreply@anthropic.com>"},
		{"grok", "grok-4.7", "Grok <noreply@x.ai>"},
		{"agy-pro", "gemini-3.1-pro", "Gemini <noreply@google.com>"},
		{"unknown", "unknown", "Maestro <noreply@maestro.local>"},
	} {
		t.Run(tc.family+"/"+tc.model, func(t *testing.T) {
			if got := Identity(tc.family, tc.model); got != tc.want {
				t.Fatalf("Identity: %s", got)
			}
		})
	}
}

func TestMessage(t *testing.T) {
	vars := Vars{Title: "Change {run}", Run: "r-0078", Task: "20c2", Family: "codex-sol", Model: "gpt-6.1-sol", Lane: "Codex <noreply@openai.com>", Summary: "Keep both sides."}
	template := "{title}\n{run} {task} {family} {model}\n{summary}\nCo-Authored-By: {lane}\n{unknown}"
	want := "Change {run}\nr-0078 20c2 codex-sol gpt-6.1-sol\nKeep both sides.\nCo-Authored-By: Codex <noreply@openai.com>\n{unknown}"
	if got := Message(template, vars); got != want {
		t.Fatalf("message: %q", got)
	}
}

func TestMergeRebasesAndCleansTask(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	a := addTask(t, r, "a")
	b := addTask(t, r, "b")
	first := commitTask(t, a, "a.txt", "a\n")
	second := commitTask(t, b, "b.txt", "b\n")
	if got := mergeTask(t, r, "a").Head; got != first {
		t.Fatalf("first merge: %s", got)
	}
	merged := mergeTask(t, r, "b").Head
	if merged == second || tgit(t, root, "rev-parse", merged+"^") != first {
		t.Fatalf("second Task was not rebased: %s", merged)
	}
	if got := tgit(t, root, "rev-parse", r.Branch); got != merged {
		t.Fatalf("Run head: %s", got)
	}
	if got := tgit(t, root, "rev-parse", "HEAD"); got != r.Base {
		t.Fatalf("user HEAD changed: %s", got)
	}
	for _, w := range []Worktree{a, b} {
		if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
			t.Fatalf("Worktree remains: %s, %v", w.Dir, err)
		}
		if _, err := head(root, "refs/heads/"+w.Branch); err == nil {
			t.Fatalf("Task branch remains: %s", w.Branch)
		}
	}
}

func TestMergeConflictLeavesTaskIntact(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	a := addTask(t, r, "a")
	b := addTask(t, r, "b")
	first := commitTask(t, a, "base.txt", "first\n")
	second := commitTask(t, b, "base.txt", "second\n")
	mergeTask(t, r, "a")
	result, err := r.Merge("b")
	if err != nil || !reflect.DeepEqual(result.Conflicts, []string{"base.txt"}) {
		t.Fatalf("conflicts: %+v, %v", result, err)
	}
	if got := tgit(t, b.Dir, "rev-parse", "HEAD"); got != second {
		t.Fatalf("Task head changed: %s", got)
	}
	if got := tgit(t, root, "rev-parse", r.Branch); got != first {
		t.Fatalf("Run head changed: %s", got)
	}
	if got := tgit(t, b.Dir, "symbolic-ref", "--short", "HEAD"); got != b.Branch {
		t.Fatalf("Task is detached: %s", got)
	}
	if dirty, err := Dirty(b.Dir); err != nil || len(dirty) != 0 {
		t.Fatalf("Task not restored: %v, %v", dirty, err)
	}
}

func TestFinish(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(map[bool]string{true: "committed", false: "uncommitted"}[commit], func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			a := addTask(t, r, "a")
			first := commitTask(t, a, "a.txt", "a\n")
			mergeTask(t, r, "a")
			b := addTask(t, r, "b")
			second := commitTask(t, b, "base.txt", "changed\n")
			mergeTask(t, r, "b")
			left := addTask(t, r, "left")
			write(t, left.Dir, "pending.txt", "pending\n")
			if err := r.Finish(commit); err != nil {
				t.Fatal(err)
			}
			if err := r.Finish(commit); err != nil {
				t.Fatalf("second Finish: %v", err)
			}
			if _, err := head(root, "refs/heads/"+r.Branch); err == nil {
				t.Fatal("Run branch remains")
			}
			if _, err := os.Stat(left.Dir); !os.IsNotExist(err) {
				t.Fatalf("remaining Worktree: %v", err)
			}
			if got := tgit(t, root, "diff", "--cached", "--name-only"); got != "" {
				t.Fatalf("user index is staged: %s", got)
			}
			if commit {
				if got := tgit(t, root, "rev-parse", "HEAD"); got != second {
					t.Fatalf("user head: %s", got)
				}
				if got := tgit(t, root, "rev-parse", "HEAD^"); got != first {
					t.Fatalf("Task commit lost: %s", got)
				}
				if paths, err := Dirty(root); err != nil || len(paths) != 0 {
					t.Fatalf("committed tree: %v, %v", paths, err)
				}
			} else {
				if got := tgit(t, root, "rev-parse", "HEAD"); got != r.Base {
					t.Fatalf("uncommitted user head: %s", got)
				}
				if paths, err := Dirty(root); err != nil || !reflect.DeepEqual(paths, []string{"a.txt", "base.txt"}) {
					t.Fatalf("uncommitted tree: %v, %v", paths, err)
				}
			}
		})
	}
}

func TestFinishRebasesOntoMovedUserBranch(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "a.txt", "a\n")
	mergeTask(t, r, "a")
	w = addTask(t, r, "b")
	commitTask(t, w, "b.txt", "b\n")
	mergeTask(t, r, "b")
	write(t, root, "user.txt", "user\n")
	tgit(t, root, "add", "user.txt")
	tgit(t, root, "commit", "-m", "Add user change")
	current := tgit(t, root, "rev-parse", "HEAD")
	if err := r.Finish(true); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, root, "rev-parse", "HEAD~2"); got != current {
		t.Fatalf("user commit lost: %s", got)
	}
	if got := tgit(t, root, "rev-list", "--count", current+"..HEAD"); got != "2" {
		t.Fatalf("Task commits lost: %s", got)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("Task change not landed: %v", err)
	}
}

func TestRebaseContinueAndAbort(t *testing.T) {
	for _, resolve := range []bool{true, false} {
		t.Run(map[bool]string{true: "continue", false: "abort"}[resolve], func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			a := addTask(t, r, "a")
			b := addTask(t, r, "b")
			commitTask(t, a, "base.txt", "first\n")
			second := commitTask(t, b, "base.txt", "second\n")
			mergeTask(t, r, "a")
			conflicts, err := r.Rebase(b)
			if err != nil || !reflect.DeepEqual(conflicts, []string{"base.txt"}) || !b.Rebasing() {
				t.Fatalf("Rebase: %v, %v", conflicts, err)
			}
			if again, err := r.Rebase(b); err != nil || !reflect.DeepEqual(again, conflicts) {
				t.Fatalf("second Rebase: %v, %v", again, err)
			}
			if _, err := r.Merge("b"); err == nil || !strings.Contains(err.Error(), "rebase is in progress") {
				t.Fatalf("Merge during a rebase: %v", err)
			}
			if !resolve {
				if err := b.Abort(); err != nil || b.Rebasing() {
					t.Fatalf("Abort: %v", err)
				}
				if got := tgit(t, b.Dir, "rev-parse", "HEAD"); got != second {
					t.Fatalf("Task head after abort: %s", got)
				}
				return
			}
			write(t, b.Dir, "base.txt", "first and second\n")
			write(t, b.Dir, "extra.txt", "extra\n")
			if left, err := b.Continue([]string{"base.txt", "extra.txt"}); err != nil || len(left) != 0 || b.Rebasing() {
				t.Fatalf("Continue: %v, %v", left, err)
			}
			if got := tgit(t, b.Dir, "log", "-1", "--format=%B"); !strings.HasPrefix(got, "Change test file") {
				t.Fatalf("message not kept: %s", got)
			}
			mergeTask(t, r, "b")
			if got := tgit(t, root, "show", r.Branch+":base.txt"); got != "first and second" {
				t.Fatalf("merged content: %s", got)
			}
			if got := tgit(t, root, "show", r.Branch+":extra.txt"); got != "extra" {
				t.Fatalf("added file: %s", got)
			}
		})
	}
}

func TestRemove(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	a := addTask(t, r, "a")
	b := addTask(t, r, "b")
	write(t, a.Dir, "pending.txt", "pending\n")
	tgit(t, b.Dir, "checkout", "--detach")
	tgit(t, root, "branch", "maestro/r-other/run")
	if _, err := Remove(root, r.ID); err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, root, "branch", "--list", "maestro/"+r.ID+"/*"); got != "" {
		t.Fatalf("Run branches remain: %s", got)
	}
	if got := tgit(t, root, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 1 {
		t.Fatalf("Worktrees remain: %s", got)
	}
	for _, dir := range []string{a.Dir, b.Dir, r.dir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("folder remains: %s, %v", dir, err)
		}
	}
	if _, err := head(root, "refs/heads/maestro/r-other/run"); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(root, r.ID); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
}

func TestLinkedWorktreeIsCleanAndMerges(t *testing.T) {
	root := repository(t)
	write(t, root, ".gitignore", "node_modules/\n")
	tgit(t, root, "add", ".gitignore")
	tgit(t, root, "commit", "-m", "Ignore dependencies")
	write(t, root, "node_modules/pkg/index.js", "dependency\n")
	r := openRun(t, root)
	w, err := r.Add("linked", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(w.Dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	if status := tgit(t, w.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("linked Worktree is dirty: %s", status)
	}
	if _, err := verify.Take(w.Dir); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	second, err := r.Add("second")
	if err != nil {
		t.Fatal(err)
	}
	if status := tgit(t, second.Dir, "status", "--porcelain"); status != "" {
		t.Fatalf("second linked Worktree is dirty: %s", status)
	}
	path := tgit(t, w.Dir, "rev-parse", "--git-path", "info/exclude")
	if !filepath.IsAbs(path) {
		path = filepath.Join(w.Dir, path)
	}
	if data, err := os.ReadFile(path); err != nil || strings.Count(string(data), "/node_modules\n") != 1 {
		t.Fatalf("exclusion file: %q, %v", data, err)
	}
	commitTask(t, w, "change.txt", "change\n")
	mergeTask(t, r, "linked")
}

func TestRemoveKeepsUnlandedTaskOfLandedRun(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	empty := addTask(t, r, "empty")
	left := addTask(t, r, "left")
	commitTask(t, left, "left.txt", "left\n")
	kept, err := Remove(root, r.ID)
	if err != nil || !reflect.DeepEqual(kept, []string{left.Branch}) {
		t.Fatalf("kept: %v, %v", kept, err)
	}
	for branch, want := range map[string]bool{r.Branch: false, empty.Branch: false, left.Branch: true} {
		if _, err := head(root, "refs/heads/"+branch); (err == nil) != want {
			t.Fatalf("branch %s exists %v, want %v", branch, err == nil, want)
		}
	}
	tgit(t, root, "branch", "-D", left.Branch)
}

func TestMergeRemoveFailureKeepsTask(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "locked")
	taskHead := commitTask(t, w, "change.txt", "change\n")
	tgit(t, root, "worktree", "lock", w.Dir)
	_, err := r.Merge("locked")
	if err == nil {
		t.Fatal("Merge removed a locked Worktree")
	}
	if got := tgit(t, root, "rev-parse", r.Branch); got != r.Base {
		t.Fatalf("Run branch moved: %s", got)
	}
	if got := tgit(t, w.Dir, "rev-parse", w.Branch); got != taskHead {
		t.Fatalf("Task branch changed: %s", got)
	}
	tgit(t, root, "worktree", "unlock", w.Dir)
	mergeTask(t, r, "locked")
}

func TestMergeRefFailureRestoresTask(t *testing.T) {
	for _, ref := range []string{"task", "run"} {
		t.Run(ref, func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "a")
			taskHead := commitTask(t, w, "change.txt", "change\n")
			branch := w.Branch
			if ref == "run" {
				branch = r.Branch
			}
			lock := tgit(t, root, "rev-parse", "--git-path", "refs/heads/"+branch+".lock")
			if !filepath.IsAbs(lock) {
				lock = filepath.Join(root, lock)
			}
			if err := os.WriteFile(lock, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := r.Merge("a")
			if err == nil {
				t.Fatal("Merge ignored the ref lock")
			}
			if got := tgit(t, w.Dir, "rev-parse", "HEAD"); got != taskHead {
				t.Fatalf("Task not restored: %s", got)
			}
			if got := tgit(t, root, "rev-parse", r.Branch); got != r.Base {
				t.Fatalf("Run moved: %s", got)
			}
			if err := os.Remove(lock); err != nil {
				t.Fatal(err)
			}
			mergeTask(t, r, "a")
		})
	}
}

func TestFinishRetriesCleanup(t *testing.T) {
	for _, commit := range []bool{true, false} {
		t.Run(map[bool]string{true: "commit", false: "squash"}[commit], func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "a")
			commitTask(t, w, "change.txt", "change\n")
			mergeTask(t, r, "a")
			left := addTask(t, r, "locked")
			tgit(t, root, "worktree", "lock", left.Dir)
			if err := r.Finish(commit); err == nil || !strings.Contains(err.Error(), "Finish remove") {
				t.Fatalf("Finish cleanup error: %v", err)
			}
			if commit {
				write(t, root, "user.txt", "new user work\n")
			}
			tgit(t, root, "worktree", "unlock", left.Dir)
			again, err := Open(root, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := again.Finish(commit); err != nil {
				t.Fatalf("Finish retry: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(root, "change.txt")); err != nil || string(data) != "change\n" {
				t.Fatalf("landed change: %q, %v", data, err)
			}
		})
	}
}

func TestRemoveKeepsUnlandedBranches(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "a")
	commitTask(t, w, "change.txt", "change\n")
	mergeTask(t, r, "a")
	left := addTask(t, r, "left")
	commitTask(t, left, "left.txt", "left\n")
	write(t, config.RunsDir(root), r.ID+"/run.json", `{"state":"paused"}`)
	kept, err := Remove(root, r.ID)
	want := []string{left.Branch, r.Branch}
	if err != nil || !reflect.DeepEqual(kept, want) {
		t.Fatalf("kept: %v, %v; want %v", kept, err, want)
	}
	for _, branch := range kept {
		if _, err := head(root, "refs/heads/"+branch); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(left.Dir); !os.IsNotExist(err) {
		t.Fatalf("Worktree remains: %v", err)
	}
}

func TestMessageEmptySummary(t *testing.T) {
	if got := Message("{title}\n\n{summary}\n\n", Vars{Title: "Title"}); got != "Title" {
		t.Fatalf("empty summary: %q", got)
	}
	template := "{title}\n\n{summary}\n\nCo-Authored-By: {lane}\n"
	if got := Message(template, Vars{Title: "Title", Lane: "Codex"}); got != "Title\n\nCo-Authored-By: Codex" {
		t.Fatalf("empty summary with trailer: %q", got)
	}
	if got := Message(template, Vars{Title: "Title", Summary: "Body.", Lane: "Codex"}); got != "Title\n\nBody.\n\nCo-Authored-By: Codex" {
		t.Fatalf("summary with trailer: %q", got)
	}
}

func TestOpenDetachedHEAD(t *testing.T) {
	root := repository(t)
	tgit(t, root, "checkout", "--detach")
	if _, err := Open(root, "detached"); !errors.Is(err, ErrNoBranch) {
		t.Fatalf("Open error: %v", err)
	}
}

func TestInvalidIDs(t *testing.T) {
	root := repository(t)
	for _, id := range []string{"", "..", "../outside", "bad ref", "bad.lock"} {
		if _, err := Open(root, id); err == nil {
			t.Errorf("Open accepted %q", id)
		}
	}
	r := openRun(t, root)
	for _, id := range []string{"", "run", "..", "../outside", "bad ref", "bad.lock"} {
		if _, err := r.Add(id); err == nil {
			t.Errorf("Add accepted %q", id)
		}
	}
}
