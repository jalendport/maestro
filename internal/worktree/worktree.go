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

// ErrNoBranch means that HEAD is detached.
var ErrNoBranch = errors.New("Open needs a user branch")

type Vars struct {
	Title, Run, Task, Family, Model, Lane, Summary string
}

// Message fills the template. It drops a line that is empty after the fill,
// keeps one blank line where the template has blank lines, and drops blank
// lines at the end.
func Message(template string, v Vars) string {
	fill := strings.NewReplacer("{title}", v.Title, "{run}", v.Run, "{task}", v.Task,
		"{family}", v.Family, "{model}", v.Model, "{lane}", v.Lane, "{summary}", v.Summary)
	var lines []string
	for _, line := range strings.Split(template, "\n") {
		filled := fill.Replace(line)
		if strings.TrimSpace(filled) == "" {
			if strings.TrimSpace(line) != "" || len(lines) == 0 || lines[len(lines)-1] == "" {
				continue
			}
			filled = ""
		}
		lines = append(lines, filled)
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
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
			stage, readErr := os.ReadFile(filepath.Join(dir, "finish"))
			if readErr != nil || string(stage) != "remove" {
				return Run{}, err
			}
		}
		return r, nil
	}
	if !os.IsNotExist(err) {
		return Run{}, err
	}
	branch, err := git(root, "", "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return Run{}, fmt.Errorf("%w: %w", ErrNoBranch, err)
	}
	r.Base, err = head(root, "HEAD")
	if err != nil {
		return Run{}, err
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

// Read reuses a detached Worktree left by a stopped supervisor.
func (r Run) Read(task string) (Worktree, error) {
	w, err := r.task(task)
	if err != nil {
		return Worktree{}, err
	}
	w.Branch = ""
	if _, err := os.Stat(filepath.Join(w.Dir, ".git")); err == nil {
		common := func(dir string) (string, error) {
			out, err := git(r.Root, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(strings.TrimSpace(out))
		}
		rootGit, err := common(r.Root)
		if err != nil {
			return Worktree{}, err
		}
		readGit, err := common(w.Dir)
		if err != nil || readGit != rootGit {
			return Worktree{}, fmt.Errorf("read Worktree at %s is not in this repository", w.Dir)
		}
		branch, err := git(r.Root, w.Dir, "branch", "--show-current")
		if err != nil || strings.TrimSpace(branch) != "" {
			return Worktree{}, fmt.Errorf("read Worktree at %s is not detached", w.Dir)
		}
		if _, err := git(r.Root, w.Dir, "reset", "--hard", r.Branch); err != nil {
			return Worktree{}, err
		}
	} else if _, err := git(r.Root, "", "worktree", "add", "--detach", w.Dir, r.Branch); err != nil {
		return Worktree{}, err
	}
	_, err = Link(r.Root, w.Dir)
	return w, err
}

var dependencies = []string{"node_modules", "vendor", ".venv", ".bundle", "storage/framework", ".env", ".env.local"}

// Link leaves a dependency folder unlinked when a touch pattern can write it.
func Link(root, dir string, touch ...[]string) (Links, error) {
	project, err := config.LoadProject(root)
	if err != nil {
		return Links{}, err
	}
	return link(root, dir, project.WorktreeLinks, touch...)
}

func link(root, dir string, extra []string, touch ...[]string) (Links, error) {
	var links Links
	root, err := filepath.Abs(root)
	if err != nil {
		return links, err
	}
	names := append(append([]string{}, dependencies...), extra...)
	// Link parents first so a nested name never creates a folder in a link.
	sort.SliceStable(names, func(i, j int) bool {
		return strings.Count(names[i], "/") < strings.Count(names[j], "/")
	})
	for _, name := range names {
		if !filepath.IsLocal(name) || filepath.Clean(name) == "." {
			return links, fmt.Errorf("invalid Worktree link %q", name)
		}
		name = filepath.ToSlash(filepath.Clean(name))
		writable := false
		for _, list := range touch {
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
		if !info.IsDir() && !info.Mode().IsRegular() {
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
		if err := exclude(root, dir, "/"+name); err != nil {
			return links, err
		}
		if err := os.Symlink(source, target); err != nil {
			return links, err
		}
		links.Linked = append(links.Linked, name)
	}
	return links, nil
}

// exclude adds rule to the exclusion file of the Worktree at dir. A project
// rule with a trailing slash, such as node_modules/, does not match a link.
func exclude(root, dir, rule string) error {
	// Doctor previews links in a temporary folder without a git checkout.
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return nil
	}
	out, err := git(root, dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(out)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == rule {
			return nil
		}
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		rule = "\n" + rule
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := fmt.Fprintln(file, rule)
	return errors.Join(writeErr, file.Close())
}

func touchesDependency(pattern, name string) bool {
	if pattern == "" || strings.HasPrefix(pattern, "!") {
		return false
	}
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	pattern = strings.Trim(pattern, "/")
	return pattern == name || strings.HasPrefix(pattern, name+"/")
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
	return r.merge(w)
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

func (r Run) merge(w Worktree) (result MergeResult, mergeErr error) {
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
	if _, err := git(r.Root, "", "worktree", "remove", "--force", w.Dir); err != nil {
		return MergeResult{}, err
	}
	// Restore the Task if branch deletion or the Run update fails.
	defer func() {
		if mergeErr == nil {
			return
		}
		if _, err := head(r.Root, "refs/heads/"+w.Branch); err != nil {
			_, err = git(r.Root, "", "branch", w.Branch, commit)
			mergeErr = errors.Join(mergeErr, err)
		}
		// A Task ref lock can cause the deletion failure. Restore the files
		// without writing that ref, then attach HEAD to the kept branch.
		_, err := git(r.Root, "", "worktree", "add", "--detach", w.Dir, commit)
		mergeErr = errors.Join(mergeErr, err)
		if err == nil {
			_, err = git(r.Root, w.Dir, "symbolic-ref", "HEAD", "refs/heads/"+w.Branch)
			mergeErr = errors.Join(mergeErr, err)
			_, err = Link(r.Root, w.Dir)
			mergeErr = errors.Join(mergeErr, err)
		}
	}()
	if _, err := git(r.Root, "", "branch", "-D", w.Branch); err != nil {
		return MergeResult{}, err
	}
	// The old head makes concurrent changes fail instead of losing commits.
	if _, err := git(r.Root, "", "update-ref", "refs/heads/"+r.Branch, commit, base); err != nil {
		return MergeResult{}, err
	}
	return MergeResult{Head: commit}, nil
}

func (r Run) Finish(commit bool) error {
	return r.FinishChecked(commit, nil)
}

// FinishChecked calls check after the landing rebase and before the merge.
// A retry after a completed merge only continues cleanup.
func (r Run) FinishChecked(commit bool, check func() error) error {
	// Keep the last completed step until cleanup succeeds. A squash leaves a
	// dirty tree, so a retry must not run the pre-merge check again.
	stagePath := filepath.Join(r.dir, "finish")
	stage, err := os.ReadFile(stagePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("Finish read step: %w", err)
	}
	if string(stage) == "remove" {
		_, err := remove(r.Root, r.ID, true)
		if err != nil {
			return fmt.Errorf("Finish remove: %w", err)
		}
		return nil
	}
	if _, err := head(r.Root, "refs/heads/"+r.Branch); err != nil {
		if _, statErr := os.Stat(r.dir); os.IsNotExist(statErr) {
			return nil
		}
		return fmt.Errorf("Finish read Run branch: %w", err)
	}
	current, err := head(r.Root, r.UserBranch)
	if err != nil {
		return fmt.Errorf("Finish read user branch: %w", err)
	}
	landed := false
	if _, err := git(r.Root, "", "merge-base", "--is-ancestor", "refs/heads/"+r.Branch, current); err == nil {
		landed = true
	}
	if landed {
		if err := os.WriteFile(stagePath, []byte("remove"), 0o644); err != nil {
			return fmt.Errorf("Finish record merge: %w", err)
		}
		_, err := remove(r.Root, r.ID, true)
		if err != nil {
			return fmt.Errorf("Finish remove: %w", err)
		}
		return nil
	}
	if string(stage) == "reset" {
		return r.finishReset(stagePath)
	}
	if err := r.checkUserBranch(); err != nil {
		return err
	}
	if string(stage) == "squash" {
		paths, err := r.landingPaths()
		if err != nil {
			return err
		}
		// A failed marker write can leave the full squash in the index.
		args := append([]string{"diff", "--cached", "--quiet", r.Branch, "--"}, literalPaths(paths)...)
		if _, err := git(r.Root, "", args...); err == nil {
			if err := os.WriteFile(stagePath, []byte("reset"), 0o644); err != nil {
				return fmt.Errorf("Finish record squash: %w", err)
			}
			return r.finishReset(stagePath)
		}
	}
	if _, err := git(r.Root, "", "merge-base", "--is-ancestor", current, "refs/heads/"+r.Branch); err != nil {
		if err := r.rebaseLanding(current); err != nil {
			return err
		}
	}
	dirty, err := Dirty(r.Root)
	if err != nil {
		return fmt.Errorf("Finish check dirty tree: %w", err)
	}
	paths, err := r.landingPaths()
	if err != nil {
		return err
	}
	var overlap []string
	for _, path := range dirty {
		for _, changed := range paths {
			if path == changed || strings.HasPrefix(path, changed+"/") || strings.HasPrefix(changed, path+"/") {
				overlap = append(overlap, path)
				break
			}
		}
	}
	if len(overlap) != 0 {
		return fmt.Errorf("landing blocked: uncommitted changes in %s overlap the Run; commit or stash them, then maestro resume %s --finish", strings.Join(overlap, ", "), r.ID)
	}
	if check != nil {
		checked, err := head(r.Root, r.Branch)
		if err != nil {
			return err
		}
		if err := check(); err != nil {
			return err
		}
		now, err := head(r.Root, r.Branch)
		if err != nil {
			return err
		}
		user, err := head(r.Root, r.UserBranch)
		if err != nil {
			return err
		}
		if now != checked || user != current {
			return fmt.Errorf("landing branches changed during the landing check; retry maestro resume %s --finish", r.ID)
		}
		if err := r.checkUserBranch(); err != nil {
			return err
		}
	}
	mode := "--ff-only"
	if !commit {
		mode = "--squash"
		if err := os.WriteFile(stagePath, []byte("squash"), 0o644); err != nil {
			return fmt.Errorf("Finish record squash start: %w", err)
		}
	}
	if _, err := git(r.Root, "", "merge", mode, r.Branch); err != nil {
		return fmt.Errorf("Finish merge: %w", err)
	}
	if !commit {
		if err := os.WriteFile(stagePath, []byte("reset"), 0o644); err != nil {
			return fmt.Errorf("Finish record squash: %w", err)
		}
		return r.finishReset(stagePath)
	}
	if err := os.WriteFile(stagePath, []byte("remove"), 0o644); err != nil {
		return fmt.Errorf("Finish record merge: %w", err)
	}
	_, err = remove(r.Root, r.ID, true)
	if err != nil {
		return fmt.Errorf("Finish remove: %w", err)
	}
	return nil
}

func (r Run) finishReset(stagePath string) error {
	if err := r.checkUserBranch(); err != nil {
		return err
	}
	paths, err := r.landingPaths()
	if err != nil {
		return err
	}
	if len(paths) > 0 {
		args := append([]string{"reset", "-q", "--"}, literalPaths(paths)...)
		if _, err := git(r.Root, "", args...); err != nil {
			return fmt.Errorf("Finish reset: %w", err)
		}
	}
	if err := os.WriteFile(stagePath, []byte("remove"), 0o644); err != nil {
		return fmt.Errorf("Finish record reset: %w", err)
	}
	_, err = remove(r.Root, r.ID, true)
	if err != nil {
		return fmt.Errorf("Finish remove: %w", err)
	}
	return nil
}

func (r Run) checkUserBranch() error {
	branch, err := git(r.Root, "", "symbolic-ref", "--quiet", "HEAD")
	if err != nil || strings.TrimSpace(branch) != r.UserBranch {
		return fmt.Errorf("user branch %s is not checked out", strings.TrimPrefix(r.UserBranch, "refs/heads/"))
	}
	return nil
}

func literalPaths(paths []string) []string {
	literal := make([]string, len(paths))
	for i, path := range paths {
		literal[i] = ":(literal)" + path
	}
	return literal
}

func (r Run) landingPaths() ([]string, error) {
	out, err := git(r.Root, "", "diff", "--no-renames", "--name-only", "-z", r.UserBranch, r.Branch)
	if err != nil {
		return nil, fmt.Errorf("Finish read paths: %w", err)
	}
	return strings.FieldsFunc(out, func(r rune) bool { return r == 0 }), nil
}

func (r Run) rebaseLanding(current string) (rebaseErr error) {
	// The common base excludes user commits from an earlier landing retry.
	base, err := git(r.Root, "", "merge-base", r.UserBranch, r.Branch)
	if err != nil {
		return fmt.Errorf("Finish read landing base: %w", err)
	}
	dir := filepath.Join(r.dir, "landing")
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
		w := Worktree{Root: r.Root, Dir: dir}
		if err := w.Abort(); err != nil {
			return fmt.Errorf("Finish abort landing: %w", err)
		}
		if _, err := git(r.Root, "", "worktree", "remove", "--force", dir); err != nil {
			return fmt.Errorf("Finish remove landing: %w", err)
		}
	}
	if _, err := git(r.Root, "", "worktree", "add", dir, r.Branch); err != nil {
		return fmt.Errorf("Finish prepare landing: %w", err)
	}
	defer func() {
		_, err := git(r.Root, "", "worktree", "remove", "--force", dir)
		rebaseErr = errors.Join(rebaseErr, err)
	}()
	if _, err := git(r.Root, dir, "rebase", "--onto", current, strings.TrimSpace(base)); err != nil {
		paths, pathErr := unmerged(r.Root, dir)
		_, abortErr := git(r.Root, dir, "rebase", "--abort")
		if len(paths) > 0 {
			err = fmt.Errorf("landing conflict in %s; Run branch %s; resolve on the Run branch, then maestro resume %s --finish", strings.Join(paths, ", "), r.Branch, r.ID)
		}
		return errors.Join(err, pathErr, abortErr)
	}
	return nil
}

// LandingWorktree gives checks a detached checkout with dependency links.
// A checkout left by a stopped check is removed before the next check.
func (r Run) LandingWorktree() (dir string, cleanup func(), err error) {
	dir = filepath.Join(r.dir, "landing")
	cleanup = func() { _, _ = git(r.Root, "", "worktree", "remove", "--force", dir) }
	if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
		if _, err := git(r.Root, "", "worktree", "remove", "--force", dir); err != nil {
			return "", nil, err
		}
	}
	if _, err := git(r.Root, "", "worktree", "add", "--detach", dir, r.Branch); err != nil {
		return "", nil, err
	}
	if _, err := Link(r.Root, dir); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

// Remove returns the branches it keeps when the Run did not land.
func Remove(root, run string) ([]string, error) {
	return remove(root, run, false)
}

func Cancel(root, run string) ([]string, error) {
	return Remove(root, run)
}

func remove(root, run string, finished bool) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dir, err := runDir(root, run)
	if err != nil {
		return nil, err
	}
	// Read only the fields needed here to avoid a dependency on the supervisor.
	var state struct {
		State string `json:"state"`
	}
	data, err := os.ReadFile(filepath.Join(config.RunsDir(root), run, "run.json"))
	if err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
		finished = finished || state.State == "finished"
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	var saved Run
	data, err = os.ReadFile(filepath.Join(dir, "run.json"))
	if err == nil {
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	userBranch := saved.UserBranch
	if userBranch == "" {
		userBranch, err = git(root, "", "symbolic-ref", "--quiet", "HEAD")
		if err == nil {
			userBranch = strings.TrimSpace(userBranch)
		}
	}
	landed := func(branch string) bool {
		if userBranch == "" {
			return false
		}
		_, err := git(root, "", "merge-base", "--is-ancestor", "refs/heads/"+branch, userBranch)
		return err == nil
	}
	runLanded := landed("maestro/" + run + "/run")
	// Git reports resolved paths, including the system's temporary folder.
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		dir = resolved
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	prefix := "refs/heads/maestro/" + run + "/"
	out, err := git(root, "", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
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
					return nil, err
				}
				path = ""
			}
		}
		if path != "" && strings.HasPrefix(field, "branch "+prefix) {
			if _, err := git(root, "", "worktree", "remove", "--force", path); err != nil {
				return nil, err
			}
		}
	}
	out, err = git(root, "", "for-each-ref", "--format=%(refname:short)", prefix)
	if err != nil {
		return nil, err
	}
	// A landed Run branch can leave a Task branch with commits that did not
	// land, so each branch must also be on the user's branch.
	var kept []string
	for _, branch := range strings.Split(strings.TrimSpace(out), "\n") {
		if branch != "" {
			if !finished && !(runLanded && landed(branch)) {
				kept = append(kept, branch)
				continue
			}
			if _, err := git(root, "", "branch", "-D", branch); err != nil {
				return kept, err
			}
		}
	}
	return kept, os.RemoveAll(dir)
}
