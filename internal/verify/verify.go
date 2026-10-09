// Package verify runs the deterministic checks after a Lane finishes: the
// project and Brief commands, the touch: scope guard against the start
// commit, and the commit range when the Brief allowed commits.
package verify

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// CheckResult is one command and its outcome.
type CheckResult struct {
	Command  string `json:"command"`
	OK       bool   `json:"ok"`
	Output   string `json:"output,omitempty"`
	Duration string `json:"duration"`
}

// RunChecks runs each command through the shell in root, one after another.
func RunChecks(root string, cmds []string, timeout time.Duration) []CheckResult {
	var out []CheckResult
	for _, c := range cmds {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		start := time.Now()
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", c)
		cmd.Dir = root
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		cancel()
		text := buf.String()
		if len(text) > 4000 {
			text = text[len(text)-4000:]
		}
		out = append(out, CheckResult{Command: c, OK: err == nil, Output: strings.TrimSpace(text), Duration: time.Since(start).Round(time.Millisecond).String()})
	}
	return out
}

// Head returns the current commit, or "" outside a git repository.
func Head(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CommitRange is "start..HEAD" when HEAD moved, else "".
func CommitRange(root, start string) string {
	head := Head(root)
	if start == "" || head == "" || head == start {
		return ""
	}
	return start[:12] + ".." + head[:12]
}

// ScopeViolations returns changed paths outside the touch globs. Paths under
// ignore (the Run folder) never count.
func ScopeViolations(changed, touch []string, ignore string) []string {
	if len(touch) == 0 {
		return nil
	}
	var out []string
	for _, p := range changed {
		if ignore != "" && (p == ignore || strings.HasPrefix(p, ignore+"/")) {
			continue
		}
		ok := false
		for _, g := range touch {
			if Match(g, p) {
				ok = true
				break
			}
		}
		if !ok {
			out = append(out, p)
		}
	}
	return out
}

// Match is a gitignore-style glob: `*` within a segment, `**` across
// segments, a trailing `/` or bare directory name covers everything under it.
func Match(glob, path string) bool {
	glob = filepath.ToSlash(strings.TrimPrefix(glob, "./"))
	path = filepath.ToSlash(path)
	if glob == path {
		return true
	}
	if strings.HasSuffix(glob, "/") {
		glob += "**"
	}
	if !strings.ContainsAny(glob, "*?") && strings.HasPrefix(path, glob+"/") {
		return true
	}
	var re strings.Builder
	re.WriteString("^")
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			i++
			if i+1 < len(glob) && glob[i+1] == '/' {
				i++
				re.WriteString("(?:.*/)?")
			} else {
				re.WriteString(".*")
			}
		case c == '*':
			re.WriteString("[^/]*")
		case c == '?':
			re.WriteString("[^/]")
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString("$")
	ok, _ := regexp.MatchString(re.String(), path)
	return ok
}
