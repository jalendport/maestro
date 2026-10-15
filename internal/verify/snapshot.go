package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Snapshot is the working tree as it stood when a Dispatch started: the
// commit, the untracked and modified paths with their modification times.
// A repository with no commits, or with uncommitted work, still gets a
// faithful "changed" list this way.
type Snapshot struct {
	Commit string               `json:"commit"`
	Taken  time.Time            `json:"taken"`
	Dirty  map[string]time.Time `json:"dirty"`
}

// SaveTo writes the snapshot as JSON so later attempts share one baseline.
func (s Snapshot) SaveTo(path string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadSnapshot reads a saved baseline.
func LoadSnapshot(path string) (Snapshot, error) {
	var s Snapshot
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Dirty == nil {
		s.Dirty = map[string]time.Time{}
	}
	return s, nil
}

// Take records the baseline for root.
func Take(root string) (Snapshot, error) {
	paths, err := statusPaths(root)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Commit: Head(root), Taken: time.Now(), Dirty: map[string]time.Time{}}
	for _, p := range paths {
		s.Dirty[p] = mtime(filepath.Join(root, p))
	}
	return s, nil
}

// Changed includes dirty paths touched during the attempt and committed changes.
func (s Snapshot) Changed(root string) []string {
	seen := map[string]bool{}
	paths, _ := statusPaths(root)
	for p := range s.Dirty {
		paths = append(paths, p)
	}
	for _, p := range paths {
		at, was := s.Dirty[p]
		if !was || !mtime(filepath.Join(root, p)).Equal(at) {
			seen[p] = true
		}
	}
	args := []string{"diff", "--name-only", "--no-renames", "-z", s.Commit + "..HEAD", "--"}
	if s.Commit == "" {
		args = []string{"ls-tree", "-r", "--name-only", "-z", "HEAD"}
	}
	b, err := exec.Command("git", append([]string{"-C", root, "-c", "core.fsmonitor=false"}, args...)...).Output()
	if err == nil {
		for _, p := range strings.Split(string(b), "\x00") {
			if p != "" {
				seen[p] = true
			}
		}
	}
	var out []string
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// statusPaths is every path git reports as modified, staged, or untracked.
func statusPaths(root string) ([]string, error) {
	b, err := exec.Command("git", "-C", root, "-c", "core.fsmonitor=false", "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return nil, fmt.Errorf("snapshot needs git and a repository at %s: %w", root, err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if len(l) <= 3 {
			continue
		}
		p := l[3:]
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		out = append(out, strings.Trim(p, `"`))
	}
	return out, nil
}

func mtime(p string) time.Time {
	st, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}
