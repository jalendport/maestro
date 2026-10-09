package verify

import (
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
	Commit string
	Taken  time.Time
	Dirty  map[string]time.Time
}

// Take records the baseline for root.
func Take(root string) Snapshot {
	s := Snapshot{Commit: Head(root), Taken: time.Now(), Dirty: map[string]time.Time{}}
	for _, p := range statusPaths(root) {
		s.Dirty[p] = mtime(filepath.Join(root, p))
	}
	return s
}

// Changed lists repo-relative paths that the Lane touched: paths dirty now
// that were clean at the start, and paths dirty at the start whose
// modification time moved after it.
func (s Snapshot) Changed(root string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range statusPaths(root) {
		at, was := s.Dirty[p]
		if was && !mtime(filepath.Join(root, p)).After(at) {
			continue
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// statusPaths is every path git reports as modified, staged, or untracked.
func statusPaths(root string) []string {
	b, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return nil
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
	return out
}

func mtime(p string) time.Time {
	st, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}
