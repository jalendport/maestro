// Package proc holds the small helpers shared by every verb that runs a child
// process: the exit-code error that rides up to main, and shell quoting for
// faithful command echoes.
package proc

import (
	"errors"
	"strings"
)

// Exit codes the CLI prototype fixed: 0 ok, 1 failed, 2 needs the Orchestrator.
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

// Quote POSIX-quotes each arg and joins them with spaces so a shell (or a
// human copy-pasting an echoed command) sees exactly the original tokens.
func Quote(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = quoteOne(a)
	}
	return strings.Join(parts, " ")
}

func quoteOne(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`&|;<>()*?![]{}#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
