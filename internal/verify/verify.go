// Package verify runs the deterministic checks after a Lane finishes: the
// project and Brief commands, the touch: scope guard against the start
// commit, and the commit range when the Brief allowed commits.
package verify

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
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
				ok = !strings.HasPrefix(g, "!")
			}
		}
		if !ok {
			out = append(out, p)
		}
	}
	return out
}

// Match uses case-sensitive gitignore patterns: * and ? stay within a segment,
// ** crosses directories only as a whole segment, bracket classes and backslash
// escapes work, slashless patterns match base names at any depth, and a leading
// / anchors to the root. Directory matches cover descendants; a trailing / matches
// only directories. A leading ! marks a negation for ScopeViolations, where the
// last matching pattern decides whether a path is in scope.
func Match(glob, path string) bool {
	glob = strings.TrimPrefix(glob, "!")
	glob = filepath.ToSlash(strings.TrimPrefix(glob, "./"))
	path = filepath.ToSlash(path)
	anchored := strings.HasPrefix(glob, "/")
	glob = strings.TrimPrefix(glob, "/")
	directory := strings.HasSuffix(glob, "/")
	glob = strings.TrimSuffix(glob, "/")
	if glob == "" || path == "" {
		return false
	}
	// filepath.Match uses ^ for a negated class; gitignore also accepts !.
	chars := []byte(glob)
	for i := 0; i < len(chars); i++ {
		if chars[i] == '\\' {
			i++
		} else if chars[i] == '[' && i+1 < len(chars) && chars[i+1] == '!' {
			chars[i+1] = '^'
		}
	}
	glob = string(chars)
	parts := strings.Split(path, "/")
	limit := len(parts)
	if directory {
		limit--
	}
	if !anchored && !strings.Contains(glob, "/") {
		for _, part := range parts[:limit] {
			if ok, _ := filepath.Match(glob, part); ok {
				return true
			}
		}
		return false
	}
	var match func([]string, []string) bool
	match = func(pattern, parts []string) bool {
		if len(pattern) == 0 {
			return len(parts) == 0
		}
		if pattern[0] == "**" {
			if len(pattern) == 1 {
				return len(parts) > 0
			}
			for i := 0; i <= len(parts); i++ {
				if match(pattern[1:], parts[i:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		ok, _ := filepath.Match(pattern[0], parts[0])
		return ok && match(pattern[1:], parts[1:])
	}
	pattern := strings.Split(glob, "/")
	for i := 1; i <= limit; i++ {
		if match(pattern, parts[:i]) {
			return true
		}
	}
	return false
}
