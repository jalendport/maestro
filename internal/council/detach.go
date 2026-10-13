package council

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Detach starts a Council supervisor that survives the caller's exit.
func Detach(folder string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find Council supervisor executable: %w", err)
	}
	logFile, err := os.Create(filepath.Join(folder, "supervisor.log"))
	if err != nil {
		return fmt.Errorf("create Council supervisor log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.Command(self, "_council", folder)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Council supervisor: %w", err)
	}
	if err := os.WriteFile(filepath.Join(folder, "supervisor.pid"), []byte(fmt.Sprint(cmd.Process.Pid)), 0o644); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("write Council supervisor pid: %w", err)
	}
	return cmd.Process.Release()
}
