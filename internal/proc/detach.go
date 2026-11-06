package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

type DetachError struct {
	Operation string
	Err       error
}

func (e *DetachError) Error() string { return e.Err.Error() }
func (e *DetachError) Unwrap() error { return e.Err }

// Detach starts a supervisor in its own session and records its pid for _council and _supervise.
func Detach(folder, verb string, appendLog bool) error {
	self, err := os.Executable()
	if err != nil {
		return &DetachError{"find", err}
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	mode := os.FileMode(0o666)
	if appendLog {
		flags = os.O_APPEND | os.O_CREATE | os.O_WRONLY
		mode = 0o644
	}
	logFile, err := os.OpenFile(filepath.Join(folder, "supervisor.log"), flags, mode)
	if err != nil {
		return &DetachError{"log", err}
	}
	defer logFile.Close()
	cmd := exec.Command(self, verb, folder)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return &DetachError{"start", err}
	}
	if verb == "_council" || verb == "_supervise" {
		if err := os.WriteFile(filepath.Join(folder, "supervisor.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return &DetachError{"pid", err}
		}
	}
	return cmd.Process.Release()
}
