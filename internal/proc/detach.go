package proc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// Detach starts a supervisor in its own session with the chosen log and pid settings.
func Detach(folder, verb string, appendLog, writePID bool) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find supervisor executable: %w", err)
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	mode := os.FileMode(0o666)
	if appendLog {
		flags = os.O_APPEND | os.O_CREATE | os.O_WRONLY
		mode = 0o644
	}
	logFile, err := os.OpenFile(filepath.Join(folder, "supervisor.log"), flags, mode)
	if err != nil {
		return fmt.Errorf("create supervisor log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.Command(self, verb, folder)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start supervisor: %w", err)
	}
	if writePID {
		if err := os.WriteFile(filepath.Join(folder, "supervisor.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("write supervisor pid: %w", err)
		}
	}
	return cmd.Process.Release()
}
