// Package proc holds the small helpers shared by every verb that runs a child
// process: the exit-code error that rides up to main and sleep prevention.
package proc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
)

// Alive reports whether a process exists, including one owned by another user.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Exit codes: 0 ok, 1 failed, 2 needs the Orchestrator.
const (
	CodeOK           = 0
	CodeFailed       = 1
	CodeOrchestrator = 2
)

// ExitError carries an exit code up to Execute so the binary can return it
// unchanged. Message is printed as an error when not empty.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string { return e.Message }

// Code extracts the exit code from an error. Returns (code, true) for an
// ExitError, else (0, false).
func Code(err error) (int, bool) {
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code, true
	}
	return 0, false
}

var awake struct {
	sync.Mutex
	callers int
	cmd     *exec.Cmd
}

// KeepAwake shares one caffeinate for this process until the last caller stops.
// It does nothing when off or when caffeinate cannot start.
func KeepAwake(on bool) func() {
	if !on {
		return nil
	}
	awake.Lock()
	defer awake.Unlock()
	if awake.callers == 0 {
		path, err := exec.LookPath("caffeinate")
		if err != nil {
			return nil
		}
		c := exec.Command(path, "-i", "-s", "-w", strconv.Itoa(os.Getpid()))
		if err := c.Start(); err != nil {
			return nil
		}
		awake.cmd = c
	}
	awake.callers++
	var once sync.Once
	return func() {
		once.Do(func() {
			awake.Lock()
			defer awake.Unlock()
			awake.callers--
			if awake.callers == 0 {
				_ = awake.cmd.Process.Kill()
				_ = awake.cmd.Wait()
				awake.cmd = nil
			}
		})
	}
}
