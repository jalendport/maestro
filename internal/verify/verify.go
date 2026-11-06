// Package verify runs the deterministic checks after a Lane finishes: the
// project and Brief commands, the touch: scope guard against the start
// commit, and the commit range when the Brief allowed commits.
package verify

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// CheckResult is one command and its outcome.
type CheckResult struct {
	Command      string `json:"command"`
	OK           bool   `json:"ok"`
	Flaky        bool   `json:"flaky,omitempty"`
	Output       string `json:"output,omitempty"`
	Duration     string `json:"duration"`
	Reason       string `json:"reason,omitempty"`
	FailureClass string `json:"failure_class,omitempty"`
	Skipped      bool   `json:"skipped,omitempty"`
}

var dockerPreflightLimit = 10 * time.Second

// RunChecks runs each command through the shell in root, one after another.
// The checks share a Wall limit budget that starts here, after the Lane ends.
// Output on stdout or stderr resets the Idle limit for the current check.
func RunChecks(root string, cmds []string, idle, budget time.Duration) []CheckResult {
	var deadline time.Time
	if budget > 0 {
		deadline = time.Now().Add(budget)
	}
	var out []CheckResult
	var stopped CheckResult
	dockerChecked := false
	for _, c := range cmds {
		if stopped.Reason != "" {
			out = append(out, CheckResult{Command: c, Skipped: true, Reason: stopped.Reason, FailureClass: stopped.FailureClass, Duration: "0s"})
			continue
		}
		if fields := strings.Fields(c); len(fields) > 0 && fields[0] == "docker" && !dockerChecked {
			dockerChecked = true
			preflightDeadline := time.Now().Add(dockerPreflightLimit)
			budgetFirst := !deadline.IsZero() && !deadline.After(preflightDeadline)
			if budgetFirst {
				preflightDeadline = deadline
			}
			preflight, _ := runCheck(root, "docker info", 0, preflightDeadline)
			if !preflight.OK {
				preflight.Command = c
				if preflight.Reason != "wall limit" || !budgetFirst {
					preflight.Reason = "Docker does not respond"
					preflight.FailureClass = "bad result"
				}
				out = append(out, preflight)
				stopped = preflight
				continue
			}
		}
		start := time.Now()
		result, normalExit := runCheck(root, c, idle, deadline)
		firstDuration := time.Since(start)
		if !result.OK && normalExit && (deadline.IsZero() || time.Until(deadline) >= firstDuration) {
			first := result
			result, _ = runCheck(root, c, idle, deadline)
			if result.FailureClass == "stall" {
				result = first
				if !deadline.IsZero() && !time.Now().Before(deadline) {
					stopped = CheckResult{Reason: "wall limit after a rerun", FailureClass: first.FailureClass}
				}
			}
			result.Duration = time.Since(start).Round(time.Millisecond).String()
			if result.OK {
				result.Flaky = true
				result.Output = first.Output
			}
		}
		out = append(out, result)
		if result.Reason != "" {
			stopped = result
		}
	}
	return out
}

// checkOutput records writes, including writes that contain no newline.
type checkOutput struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	last time.Time
}

func (o *checkOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(p) > 0 {
		o.last = time.Now()
	}
	return o.buf.Write(p)
}

func runCheck(root, command string, idle time.Duration, deadline time.Time) (result CheckResult, normalExit bool) {
	start := time.Now()
	result.Command = command
	defer func() {
		result.Duration = time.Since(start).Round(time.Millisecond).String()
	}()
	if !deadline.IsZero() && !start.Before(deadline) {
		result.Reason, result.FailureClass = "wall limit", "stall"
		return result, normalExit
	}
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	output := &checkOutput{last: start}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		result.Output = err.Error()
		return result, normalExit
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var idleC, wallC <-chan time.Time
	var idleTimer *time.Timer
	if idle > 0 {
		idleTimer = time.NewTimer(idle)
		defer idleTimer.Stop()
		idleC = idleTimer.C
	}
	if !deadline.IsZero() {
		wallTimer := time.NewTimer(time.Until(deadline))
		defer wallTimer.Stop()
		wallC = wallTimer.C
	}
loop:
	for {
		select {
		case err := <-done:
			result.OK = err == nil
			normalExit = cmd.ProcessState.Exited()
			break loop
		case <-wallC:
			result.Reason = "wall limit"
		case <-idleC:
			output.mu.Lock()
			remaining := idle - time.Since(output.last)
			output.mu.Unlock()
			if remaining > 0 {
				idleTimer.Reset(remaining)
				continue
			}
			result.Reason = "no output for " + idle.String()
			if !deadline.IsZero() && !time.Now().Before(deadline) {
				result.Reason = "wall limit"
			}
		}
		// A command that already ended keeps its exit result.
		select {
		case err := <-done:
			result.OK = err == nil
			normalExit = cmd.ProcessState.Exited()
			result.Reason = ""
		default:
			result.FailureClass = "stall"
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		break
	}
	output.mu.Lock()
	result.Output = recordedOutput(output.buf.String(), !result.OK)
	output.mu.Unlock()
	if !result.OK && normalExit {
		result.FailureClass = "bad result"
	}
	return result, normalExit
}

func recordedOutput(text string, failed bool) string {
	if len(text) <= 4000 {
		return strings.TrimSpace(text)
	}
	tail := text[len(text)-4000:]
	if !failed {
		return strings.TrimSpace(tail)
	}
	var failures strings.Builder
	for _, line := range strings.Split(text, "\n") {
		for _, marker := range []string{"--- FAIL", "FAIL\t", "panic:", "Error:", "error:", "✗", "not ok"} {
			if !strings.Contains(line, marker) {
				continue
			}
			line += "\n"
			if remaining := 2000 - failures.Len(); len(line) > remaining {
				line = line[:remaining]
			}
			failures.WriteString(line)
			break
		}
		if failures.Len() == 2000 {
			break
		}
	}
	return strings.TrimSpace(strings.TrimRight(failures.String(), "\n") + "\n" + tail)
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

// ScopeViolations returns changed paths outside InScope. Deleted paths must
// match touch itself. Paths under ignore (the Run folder) never count.
func ScopeViolations(changed, touch []string, ignore string, deleted ...string) []string {
	if len(touch) == 0 {
		return nil
	}
	var out []string
	for _, p := range changed {
		if ignore != "" && (p == ignore || strings.HasPrefix(p, ignore+"/")) {
			continue
		}
		removed := false
		for _, path := range deleted {
			removed = removed || path == p
		}
		if !InScope(p, touch) || (removed && !InTouch(p, touch)) {
			out = append(out, p)
		}
	}
	return out
}

// InScope reports whether path is inside the touch globs or is a test file.
// An empty touch permits every path. Test files stay in scope even when a
// negation excludes them; other paths use the last matching touch pattern.
func InScope(path string, touch []string) bool {
	if InTouch(path, touch) {
		return true
	}
	for _, pattern := range []string{
		"**/*_test.go", "**/*_test.*", "**/*.test.*", "**/*.spec.*", "**/*Test.php",
		"**/test/**", "**/tests/**", "**/__tests__/**", "**/spec/**", "**/testdata/**",
	} {
		if Match(pattern, path) {
			return true
		}
	}
	return false
}

// InTouch reports whether the Brief's own patterns permit the path.
func InTouch(path string, touch []string) bool {
	if len(touch) == 0 {
		return true
	}
	ok := false
	for _, pattern := range touch {
		if Match(pattern, path) {
			ok = !strings.HasPrefix(pattern, "!")
		}
	}
	return ok
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
