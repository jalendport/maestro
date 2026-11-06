package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Landed reports whether all branches and uncommitted work of an identity
// are already on userBranch. It does not change the repository.
func Landed(root, id, userBranch string) (landed bool, reason string, err error) {
	_, _, reason, err = landedIdentity(root, id, userBranch)
	return err == nil && reason == "", reason, err
}

// RemoveLanded checks the identity again before removing its Worktrees,
// branches, and folder. It leaves an unlanded identity unchanged.
func RemoveLanded(root, id, userBranch string) error {
	dir, trees, reason, err := landedIdentity(root, id, userBranch)
	if err != nil {
		return err
	}
	if reason != "" {
		return fmt.Errorf("identity %s did not land: %s", id, reason)
	}
	for _, tree := range trees {
		if _, err := git(root, "", "worktree", "remove", "--force", tree); err != nil {
			return err
		}
	}
	branches, err := identityBranches(root, id)
	if err != nil {
		return err
	}
	for _, branch := range branches {
		if _, err := git(root, "", "branch", "-D", strings.TrimPrefix(branch, "refs/heads/")); err != nil {
			return err
		}
	}
	return os.RemoveAll(dir)
}

func identityBranches(root, id string) ([]string, error) {
	out, err := git(root, "", "for-each-ref", "--format=%(refname)", "refs/heads/maestro/"+id+"/")
	return strings.Fields(out), err
}

func landedIdentity(root, id, userBranch string) (dir string, trees []string, reason string, err error) {
	dir, err = runDir(root, id)
	if err != nil {
		return
	}
	target, err := head(root, userBranch+"^{commit}")
	if err != nil {
		return dir, nil, "", err
	}
	branches, err := identityBranches(root, id)
	if err != nil {
		return dir, nil, "", err
	}
	for _, branch := range branches {
		if _, err := git(root, "", "merge-base", "--is-ancestor", branch, target); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 1 {
				return dir, nil, "branch " + strings.TrimPrefix(branch, "refs/heads/") + " has commits that did not land", nil
			}
			return dir, nil, "", err
		}
	}
	// Git lists resolved paths, including the system temporary folder.
	if resolved, resolveErr := filepath.EvalSymlinks(dir); resolveErr == nil {
		dir = resolved
	} else if !os.IsNotExist(resolveErr) {
		return dir, nil, "", resolveErr
	}
	out, err := git(root, "", "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return dir, nil, "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return dir, nil, "", err
	}
	for _, record := range strings.Split(out, "\x00\x00") {
		var path, branch string
		for _, field := range strings.Split(record, "\x00") {
			if strings.HasPrefix(field, "worktree ") {
				path = strings.TrimPrefix(field, "worktree ")
			} else if strings.HasPrefix(field, "branch ") {
				branch = strings.TrimPrefix(field, "branch ")
			}
		}
		rel, relErr := filepath.Rel(dir, path)
		inside := relErr == nil && filepath.IsLocal(rel) && rel != "."
		if !inside && !strings.HasPrefix(branch, "refs/heads/maestro/"+id+"/") {
			continue
		}
		if path == resolvedRoot {
			return dir, nil, "identity branch is checked out in the project tree", nil
		}
		trees = append(trees, path)
	}
	sort.Strings(trees)
	for _, tree := range trees {
		paths, err := Dirty(tree)
		if err != nil {
			return dir, nil, "", err
		}
		for _, path := range paths {
			ok, err := landedPath(root, tree, target, path)
			if err != nil {
				return dir, nil, "", err
			}
			if !ok {
				return dir, nil, "uncommitted changes in " + filepath.Join(tree, path), nil
			}
		}
		// Compare staged changes separately: a matching file on disk must not
		// hide different content saved in the index.
		staged, err := git(root, tree, "diff", "--cached", "--no-renames", "--name-only", "-z", "HEAD", "--")
		if err != nil {
			return dir, nil, "", err
		}
		for _, path := range strings.Split(staged, "\x00") {
			if path == "" {
				continue
			}
			_, err := git(root, tree, "diff", "--cached", "--quiet", target, "--", ":(literal)"+path)
			if err != nil {
				var exit *exec.ExitError
				if errors.As(err, &exit) && exit.ExitCode() == 1 {
					return dir, nil, "uncommitted changes in " + filepath.Join(tree, path), nil
				}
				return dir, nil, "", err
			}
		}
	}
	return dir, trees, "", nil
}

func landedPath(root, tree, target, path string) (bool, error) {
	out, err := git(root, "", "ls-tree", "-z", target, "--", ":(literal)"+path)
	if err != nil {
		return false, err
	}
	info, statErr := os.Lstat(filepath.Join(tree, path))
	if os.IsNotExist(statErr) {
		return out == "", nil
	}
	if statErr != nil {
		return false, statErr
	}
	if out == "" {
		return false, nil
	}
	metadata, _, _ := strings.Cut(out, "\t")
	fields := strings.Fields(metadata)
	if len(fields) != 3 || fields[1] != "blob" {
		return false, nil
	}
	mode := "100644"
	var content []byte
	if info.Mode()&os.ModeSymlink != 0 {
		mode = "120000"
		link, readErr := os.Readlink(filepath.Join(tree, path))
		content, err = []byte(link), readErr
	} else if info.Mode().IsRegular() {
		if info.Mode()&0o111 != 0 {
			mode = "100755"
		}
		content, err = os.ReadFile(filepath.Join(tree, path))
	} else {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	blob, err := git(root, "", "cat-file", "blob", fields[2])
	return mode == fields[0] && string(content) == blob, err
}
