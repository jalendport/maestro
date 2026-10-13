// Package proc holds the small helpers shared by every verb that runs a child
// process: the exit-code error that rides up to main and sleep prevention.
package proc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

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

// KeepAwake starts caffeinate tied to this process; no-op when off or absent.
func KeepAwake(on bool) func() {
	if !on {
		return nil
	}
	path, err := exec.LookPath("caffeinate")
	if err != nil {
		return nil
	}
	c := exec.Command(path, "-s", "-w", strconv.Itoa(os.Getpid()))
	if err := c.Start(); err != nil {
		return nil
	}
	return func() { _ = c.Process.Kill(); _, _ = c.Process.Wait() }
}
