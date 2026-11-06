// Package worktree owns the git operations for Run branches and Task branches.
package worktree

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jalendport/maestro/internal/config"
)

type Run struct {
	Root       string
	ID         string
	Branch     string
	Base       string
	UserBranch string
	dir        string
}

type Worktree struct {
	Root   string
	Dir    string
	Branch string
}

type Links struct {
	Linked  []string
	Refused []string
}

type Vars struct {
	Title, Run, Task, Family, Model, Lane, Summary string
}

func Message(template string, v Vars) string {
	return strings.NewReplacer("{title}", v.Title, "{run}", v.Run, "{task}", v.Task,
		"{family}", v.Family, "{model}", v.Model, "{lane}", v.Lane, "{summary}", v.Summary).Replace(template)
}

type MergeResult struct {
	Head      string
	Conflicts []string
}

func git(root, dir string, args ...string) (string, error) {
	command := []string{"-C", root, "-c", "core.fsmonitor=false"}
	if dir != "" {
		command = append(command, "-C", dir)
	}
	out, err := exec.Command("git", append(command, args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func head(root, ref string) (string, error) {
	out, err := git(root, "", "rev-parse", "--verify", ref)
	return strings.TrimSpace(out), err
}

func runDir(root, run string) (string, error) {
	if run == "" || strings.ContainsAny(run, `/\\`) || run == "." || run == ".." {
		return "", fmt.Errorf("invalid Run id %q", run)
	}
	if _, err := git(root, "", "check-ref-format", "--branch", "maestro/"+run+"/run"); err != nil {
		return "", err
	}
	control, err := config.ControlDirForFolder(filepath.Join(root, ".agents", "maestro", "runs", run))
	if err != nil {
		return "", err
	}
	return filepath.Join(config.Home(), "worktrees", filepath.Base(filepath.Dir(control)), run), nil
}

func Open(root, run string) (Run, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Run{}, err
	}
	dir, err := runDir(root, run)
	if err != nil {
		return Run{}, err
	}
	r := Run{Root: root, ID: run, Branch: "maestro/" + run + "/run", dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err == nil {
		var saved Run
		if err := json.Unmarshal(data, &saved); err != nil {
			return Run{}, err
		}
		r.Base, r.UserBranch = saved.Base, saved.UserBranch
		if _, err := head(root, "refs/heads/"+r.Branch); err != nil {
			return Run{}, err
		}
		return r, nil
	}
	if !os.IsNotExist(err) {
		return Run{}, err
	}
	r.Base, err = head(root, "HEAD")
	if err != nil {
		return Run{}, err
	}
	branch, err := git(root, "", "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return Run{}, fmt.Errorf("Open needs a user branch: %w", err)
	}
	r.UserBranch = strings.TrimSpace(branch)
	if _, err := head(root, "refs/heads/"+r.Branch); err != nil {
		if _, err := git(root, "", "branch", r.Branch, r.Base); err != nil {
			return Run{}, err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Run{}, err
	}
	data, err = json.Marshal(r)
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "run.json"), data, 0o644)
	}
	return r, err
}

func Dirty(root string) ([]string, error) {
	out, err := git(root, "", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	var paths []string
	entries := strings.Split(out, "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		if strings.ContainsAny(entry[:2], "RC") {
			i++
			if i < len(entries) {
				paths = append(paths, entries[i])
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (r Run) task(task string) (Worktree, error) {
	if task == "" || task == "run" || task == "." || task == ".." || strings.ContainsAny(task, `/\\`) {
		return Worktree{}, fmt.Errorf("invalid Task id %q", task)
	}
	w := Worktree{Root: r.Root, Dir: filepath.Join(r.dir, task), Branch: "maestro/" + r.ID + "/" + task}
	_, err := git(r.Root, "", "check-ref-format", "--branch", w.Branch)
	return w, err
}

// Add accepts the touch list so a shared dependency folder stays read-only.
func (r Run) Add(task string, touch ...[]string) (Worktree, error) {
	w, err := r.task(task)
	if err != nil {
		return Worktree{}, err
	}
	if _, err := os.Stat(filepath.Join(w.Dir, ".git")); err == nil {
		branch, err := git(r.Root, w.Dir, "symbolic-ref", "--short", "HEAD")
		if err != nil || strings.TrimSpace(branch) != w.Branch {
			return Worktree{}, fmt.Errorf("Worktree at %s is not on %s", w.Dir, w.Branch)
		}
		return w, nil
	}
	args := []string{"worktree", "add"}
	if _, err := head(r.Root, "refs/heads/"+w.Branch); err != nil {
		args = append(args, "-b", w.Branch, w.Dir, r.Branch)
	} else {
		args = append(args, w.Dir, w.Branch)
	}
	if _, err := git(r.Root, "", args...); err != nil {
		return Worktree{}, err
	}
	_, err = Link(r.Root, w.Dir, touch...)
	return w, err
}

var dependencies = []string{"node_modules", "vendor", ".venv", ".bundle", "storage/framework"}

func (r Run) AddInTree(task string) (Worktree, error) {
	w, err := r.task(task)
	if err != nil {
		return w, err
	}
	w.Dir = r.Root
	branch, err := git(r.Root, "", "symbolic-ref", "--short", "HEAD")
	if err == nil && strings.TrimSpace(branch) == w.Branch {
		return w, nil
	}
	if _, err := head(r.Root, "refs/heads/"+w.Branch); err != nil {
		if _, err := git(r.Root, "", "branch", w.Branch, r.Branch); err != nil {
			return w, err
		}
	}
	_, err = git(r.Root, "", "checkout", w.Branch)
	return w, err
}

// Link leaves a dependency folder unlinked when a touch pattern can write it.
func Link(root, dir string, touch ...[]string) (Links, error) {
	var links Links
	root, err := filepath.Abs(root)
	if err != nil {
		return links, err
	}
	for _, name := range dependencies {
		writable := false
		for _, list := range touch {
			if len(list) == 0 {
				writable = true
			}
			for _, pattern := range list {
				if touchesDependency(pattern, name) {
					writable = true
				}
			}
		}
		source := filepath.Join(root, name)
		info, err := os.Stat(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return links, err
		}
		if !info.IsDir() {
			continue
		}
		if _, err := git(root, "", "check-ignore", "--quiet", "--", name); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 1 {
				continue
			}
			return links, err
		}
		if writable {
			links.Refused = append(links.Refused, name)
			continue
		}
		if dir == "" {
			continue
		}
		target := filepath.Join(dir, name)
		if _, err := os.Lstat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return links, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return links, err
		}
		if err := os.Symlink(source, target); err != nil {
			return links, err
		}
		links.Linked = append(links.Linked, name)
	}
	return links, nil
}

func touchesDependency(pattern, name string) bool {
	if pattern == "" || strings.HasPrefix(pattern, "!") {
		return false
	}
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.Trim(pattern, "/")
	if pattern == "" {
		return false
	}
	// A slashless pattern can match a name anywhere inside the dependency.
	if !anchored && !strings.Contains(pattern, "/") {
		return true
	}
	chars := []byte(pattern)
	for i := 0; i < len(chars); i++ {
		if chars[i] == '\\' {
			i++
		} else if chars[i] == '[' && i+1 < len(chars) && chars[i+1] == '!' {
			chars[i+1] = '^'
		}
	}
	return overlaps(strings.Split(string(chars), "/"), strings.Split(name, "/"))
}

func overlaps(pattern, dir []string) bool {
	if len(pattern) == 0 || len(dir) == 0 {
		return true
	}
	if pattern[0] == "**" {
		return overlaps(pattern[1:], dir) || overlaps(pattern, dir[1:])
	}
	if len(pattern) == 1 {
		for _, part := range dir {
			if ok, _ := filepath.Match(pattern[0], part); ok {
				return true
			}
		}
	}
	ok, _ := filepath.Match(pattern[0], dir[0])
	return ok && overlaps(pattern[1:], dir[1:])
}

func (w Worktree) Commit(message string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", nil
	}
	literal := make([]string, len(paths))
	for i, path := range paths {
		literal[i] = ":(literal)" + path
	}
	if _, err := git(w.Root, w.Dir, append([]string{"add", "--"}, literal...)...); err != nil {
		return "", err
	}
	// --only keeps staged work outside paths out of this commit.
	if _, err := git(w.Root, w.Dir, append([]string{"commit", "--only", "-m", message, "--"}, literal...)...); err != nil {
		return "", err
	}
	out, err := git(w.Root, w.Dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

func Identity(family, model string) string {
	name, email := "Maestro", "noreply@maestro.local"
	switch {
	case strings.HasPrefix(family, "codex-"):
		name, email = "Codex", "noreply@openai.com"
	case strings.HasPrefix(family, "claude-"):
		name, email = "Claude", "noreply@anthropic.com"
		parts := strings.Split(strings.TrimPrefix(model, "claude-"), "-")
		if len(parts) > 0 {
			switch parts[0] {
			case "opus", "sonnet", "haiku", "fable":
				name += " " + strings.ToUpper(parts[0][:1]) + parts[0][1:]
				if len(parts) > 1 {
					version := parts[1:]
					if len(version[len(version)-1]) == 8 {
						version = version[:len(version)-1]
					}
					if len(version) > 0 {
						name += " " + strings.Join(version, ".")
					}
				}
			}
		}
	case strings.HasPrefix(family, "grok"):
		name, email = "Grok", "noreply@x.ai"
	case strings.HasPrefix(family, "agy-"):
		name, email = "Gemini", "noreply@google.com"
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

func (r Run) Merge(task string) (MergeResult, error) {
	w, err := r.task(task)
	if err != nil {
		return MergeResult{}, err
	}
	return r.merge(w, false)
}

func (r Run) MergeTree(task string) (MergeResult, error) {
	w, err := r.task(task)
	if err != nil {
		return MergeResult{}, err
	}
	w.Dir = r.Root
	return r.merge(w, true)
}

func unmerged(root, dir string) ([]string, error) {
	out, err := git(root, dir, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(out, func(r rune) bool { return r == 0 }), nil
}

// Rebasing reports whether a rebase stopped in the Worktree.
func (w Worktree) Rebasing() bool {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		out, err := git(w.Root, w.Dir, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		path := strings.TrimSpace(out)
		if !filepath.IsAbs(path) {
			path = filepath.Join(w.Dir, path)
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// Rebase starts a rebase of the Task branch onto the Run branch for a
// conflict attempt. It stops at the conflicts and returns their paths, so a
// Lane edits only files; a Lane cannot write the git folder. No conflicts
// means the rebase is complete.
func (r Run) Rebase(w Worktree) ([]string, error) {
	if w.Rebasing() {
		return unmerged(r.Root, w.Dir)
	}
	base, err := head(r.Root, "refs/heads/"+r.Branch)
	if err != nil {
		return nil, err
	}
	if _, rebaseErr := git(r.Root, w.Dir, "rebase", base); rebaseErr != nil {
		conflicts, err := unmerged(r.Root, w.Dir)
		if err != nil {
			return nil, err
		}
		if len(conflicts) == 0 {
			_, _ = git(r.Root, w.Dir, "rebase", "--abort")
			return nil, rebaseErr
		}
		return conflicts, nil
	}
	return nil, nil
}

// Continue adds paths and continues the stopped rebase with the commit
// messages it has. It returns the paths of a new stop.
func (w Worktree) Continue(paths []string) ([]string, error) {
	if len(paths) > 0 {
		literal := make([]string, len(paths))
		for i, path := range paths {
			literal[i] = ":(literal)" + path
		}
		if _, err := git(w.Root, w.Dir, append([]string{"add", "-A", "--"}, literal...)...); err != nil {
			return nil, err
		}
	}
	if conflicts, err := unmerged(w.Root, w.Dir); err != nil || len(conflicts) > 0 {
		return conflicts, err
	}
	if _, err := git(w.Root, w.Dir, "-c", "core.editor=true", "rebase", "--continue"); err != nil {
		conflicts, uerr := unmerged(w.Root, w.Dir)
		if uerr != nil || len(conflicts) == 0 {
			return nil, err
		}
		return conflicts, nil
	}
	return nil, nil
}

// Abort stops a rebase that Rebase started and keeps the Task branch.
func (w Worktree) Abort() error {
	if !w.Rebasing() {
		return nil
	}
	_, err := git(w.Root, w.Dir, "rebase", "--abort")
	return err
}

func (r Run) merge(w Worktree, inTree bool) (MergeResult, error) {
	if w.Rebasing() {
		return MergeResult{}, fmt.Errorf("a rebase is in progress in %s; finish it, then merge again", w.Dir)
	}
	base, err := head(r.Root, "refs/heads/"+r.Branch)
	if err != nil {
		return MergeResult{}, err
	}
	if _, rebaseErr := git(r.Root, w.Dir, "rebase", base); rebaseErr != nil {
		conflicts, err := unmerged(r.Root, w.Dir)
		if err != nil {
			return MergeResult{}, err
		}
		if len(conflicts) == 0 {
			return MergeResult{}, rebaseErr
		}
		if _, err := git(r.Root, w.Dir, "rebase", "--abort"); err != nil {
			return MergeResult{}, err
		}
		return MergeResult{Conflicts: conflicts}, nil
	}
	commit, err := head(r.Root, "refs/heads/"+w.Branch)
	if err != nil {
		return MergeResult{}, err
	}
	// The old head makes concurrent changes fail instead of losing commits.
	if _, err := git(r.Root, "", "update-ref", "refs/heads/"+r.Branch, commit, base); err != nil {
		return MergeResult{}, err
	}
	if inTree {
		if _, err := git(r.Root, "", "checkout", strings.TrimPrefix(r.UserBranch, "refs/heads/")); err != nil {
			return MergeResult{Head: commit}, err
		}
	} else {
		if _, err := git(r.Root, "", "worktree", "remove", w.Dir); err != nil {
			return MergeResult{Head: commit}, err
		}
	}
	_, err = git(r.Root, "", "branch", "-D", w.Branch)
	return MergeResult{Head: commit}, err
}

func (r Run) Finish(commit bool) error {
	current, err := head(r.Root, r.UserBranch)
	if err != nil {
		return err
	}
	// The Run branch must contain the user's branch. A user branch that moved
	// lands after the Run branch is rebased onto it.
	if current != r.Base {
		if _, err := git(r.Root, "", "merge-base", "--is-ancestor", current, "refs/heads/"+r.Branch); err != nil {
			return fmt.Errorf("user branch moved: base %s, current %s; rebase Run branch %s onto the user branch, or reset the user branch to the base", r.Base, current, r.Branch)
		}
	}
	branch, err := git(r.Root, "", "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(branch) != r.UserBranch {
		return fmt.Errorf("Finish needs user branch %s checked out", r.UserBranch)
	}
	dirty, err := Dirty(r.Root)
	if err != nil {
		return err
	}
	if len(dirty) != 0 {
		return fmt.Errorf("Finish needs a clean user tree: %s", strings.Join(dirty, ", "))
	}
	mode := "--ff-only"
	if !commit {
		mode = "--squash"
	}
	if _, err := git(r.Root, "", "merge", mode, r.Branch); err != nil {
		return err
	}
	if !commit {
		if _, err := git(r.Root, "", "reset", "--mixed", "HEAD"); err != nil {
			return err
		}
	}
	return Remove(r.Root, r.ID)
}

func Remove(root, run string) error {
	return remove(root, run, false)
}

func Cancel(root, run string) error {
	return remove(root, run, true)
}

func remove(root, run string, keepRun bool) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	dir, err := runDir(root, run)
	if err != nil {
		return err
	}
	// Git reports resolved paths, including the system's temporary folder.
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		dir = resolved
	} else if !os.IsNotExist(err) {
		return err
	}
	prefix := "refs/heads/maestro/" + run + "/"
	out, err := git(root, "", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	var path string
	for _, field := range strings.Split(out, "\x00") {
		if strings.HasPrefix(field, "worktree ") {
			path = strings.TrimPrefix(field, "worktree ")
			resolvedRoot, _ := filepath.EvalSymlinks(root)
			if path == root || path == resolvedRoot {
				path = ""
				continue
			}
			rel, err := filepath.Rel(dir, path)
			if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				if _, err := git(root, "", "worktree", "remove", "--force", path); err != nil {
					return err
				}
				path = ""
			}
		}
		if path != "" && strings.HasPrefix(field, "branch "+prefix) {
			if _, err := git(root, "", "worktree", "remove", "--force", path); err != nil {
				return err
			}
		}
	}
	out, err = git(root, "", "for-each-ref", "--format=%(refname:short)", prefix)
	if err != nil {
		return err
	}
	for _, branch := range strings.Split(strings.TrimSpace(out), "\n") {
		if keepRun && branch == "maestro/"+run+"/run" {
			continue
		}
		if branch != "" {
			if _, err := git(root, "", "branch", "-D", branch); err != nil {
				return err
			}
		}
	}
	return os.RemoveAll(dir)
}
