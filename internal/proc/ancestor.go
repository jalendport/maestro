package proc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Process is the part of a process record used to find the Orchestrator.
type Process struct {
	Parent     int
	Executable string
	Started    string
}

// LookupProcess reads the parent pid and executable without starting a Harness.
func LookupProcess(pid int) (Process, error) {
	identity, err := SnapshotProcess(pid)
	if err != nil {
		return Process{}, err
	}
	out, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "ppid=", "-o", "comm=").Output()
	if err != nil {
		return Process{}, err
	}
	line := strings.TrimSpace(string(out))
	parent, executable, ok := strings.Cut(line, " ")
	if !ok {
		return Process{}, fmt.Errorf("no process record for %d", pid)
	}
	ppid, err := strconv.Atoi(parent)
	if err != nil {
		return Process{}, err
	}
	// Linux comm can be shortened. /proc has the full executable path.
	if path, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		executable = path
	}
	return Process{Parent: ppid, Executable: strings.TrimSpace(executable), Started: identity.Started}, nil
}

// Ancestor returns the nearest process whose executable is one of bins.
// lookup is injected so tests do not need a live Harness.
func Ancestor(pid int, bins []string, lookup func(int) (Process, error)) int {
	wanted := map[string]bool{}
	for _, bin := range bins {
		if bin != "" {
			wanted[executablePath(bin)] = true
		}
	}
	seen := map[int]bool{}
	for pid > 1 && !seen[pid] {
		seen[pid] = true
		p, err := lookup(pid)
		if err != nil {
			return 0
		}
		if wanted[executablePath(p.Executable)] {
			return pid
		}
		pid = p.Parent
	}
	return 0
}

func executablePath(path string) string {
	if path == "" {
		return ""
	}
	if found, err := exec.LookPath(path); err == nil {
		path = found
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	return path
}
