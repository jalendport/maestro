package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jalendport/maestro/internal/verify"
)

type attemptBaseline struct {
	verify.Snapshot
	Contents map[string][]byte `json:"contents"`
}

func takeBaseline(root, folder string) (attemptBaseline, error) {
	snapshot, err := verify.Take(root)
	if err != nil {
		return attemptBaseline{}, err
	}
	s := attemptBaseline{Snapshot: snapshot, Contents: map[string][]byte{}}
	for p := range s.Dirty {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil && !os.IsNotExist(err) {
			return s, err
		}
		s.Contents[p] = b
	}
	b, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	return s, os.WriteFile(filepath.Join(folder, FileBaseline), b, 0o644)
}

func (s attemptBaseline) diff(root string, changed []string) (string, error) {
	var clean, dirty []string
	for _, p := range changed {
		if _, ok := s.Contents[p]; ok {
			dirty = append(dirty, p)
		} else {
			clean = append(clean, p)
		}
	}
	var w strings.Builder
	w.WriteString(collectDiff(root, s.Commit, clean))
	if len(dirty) == 0 {
		return w.String(), nil
	}
	folder, err := os.MkdirTemp("", "maestro-diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(folder)
	sort.Strings(dirty)
	for _, p := range dirty {
		if w.Len() > maxDiffBytes {
			break
		}
		after, err := os.ReadFile(filepath.Join(root, p))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		before := s.Contents[p]
		if isBinary(before) || isBinary(after) || (before == nil && after == nil) {
			continue
		}
		paths := []string{os.DevNull, os.DevNull}
		for i, contents := range [][]byte{before, after} {
			if contents == nil {
				continue
			}
			side := []string{"a", "b"}[i]
			paths[i] = filepath.Join(side, p)
			path := filepath.Join(folder, paths[i])
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(path, contents, 0o644); err != nil {
				return "", err
			}
		}
		cmd := exec.Command("git", "-c", "core.fsmonitor=false", "-c", "diff.external=", "diff", "--no-ext-diff", "--no-textconv", "--no-index", "--no-prefix", "--", paths[0], paths[1])
		cmd.Dir = folder
		b, err := cmd.Output()
		if err != nil && (cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 1) {
			return "", fmt.Errorf("attempt diff for %s: %w", p, err)
		}
		w.Write(b)
	}
	diff := w.String()
	if len(diff) > maxDiffBytes {
		diff = diff[:maxDiffBytes] + "\n[diff cut]\n"
	}
	return diff, nil
}

// inherit carries the earlier attempts' work into this attempt's baseline.
// Each path an earlier attempt changed is measured against the tree as it
// stood before that attempt, and counts as changed even when this attempt
// leaves it alone, so the gate and the end-of-run Council see the Task's
// whole diff and not the last attempt's slice of it. The earliest attempt
// wins for a path that several attempts changed.
func (s *attemptBaseline) inherit(folder string, attempt int) error {
	// The earliest attempt's commit is the Task's start, and an empty value
	// there means the repository had no commit yet: it must still win.
	commit, inherited := "", false
	for n := attempt - 1; n >= 1; n-- {
		dir := filepath.Join(folder, FileAttempts, strconv.Itoa(n))
		rec, err := LoadRecord(dir)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, FileBaseline))
		if err != nil {
			continue
		}
		var earlier attemptBaseline
		if json.Unmarshal(b, &earlier) != nil {
			continue
		}
		for _, p := range rec.Verification.Changed {
			if before, ok := earlier.Contents[p]; ok {
				s.Contents[p] = before
			} else {
				delete(s.Contents, p)
			}
			if at, ok := earlier.Dirty[p]; ok {
				s.Dirty[p] = at
			} else {
				delete(s.Dirty, p)
			}
		}
		commit, inherited = earlier.Commit, true
	}
	if inherited {
		s.Commit = commit
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(folder, FileBaseline), b, 0o644)
}
