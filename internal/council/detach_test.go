package council

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const detachTestEnv = "MAESTRO_COUNCIL_DETACH_TEST"

func TestMain(m *testing.M) {
	if os.Getenv(detachTestEnv) != "" && len(os.Args) == 3 && os.Args[1] == "_council" {
		folder := os.Args[2]
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(folder, "caller-exited")); err == nil {
				group, err := syscall.Getpgid(0)
				if err != nil {
					os.Exit(1)
				}
				fmt.Fprintf(os.Stdout, "pid=%d group=%d\n", os.Getpid(), group)
				fmt.Fprintln(os.Stderr, "Council supervisor completed after caller exit")
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestDetachCallerProcess(t *testing.T) {
	folder := os.Getenv(detachTestEnv)
	if folder == "" {
		return
	}
	if err := Detach(folder); err != nil {
		t.Fatal(err)
	}
}

func TestDetachSurvivesCallerExit(t *testing.T) {
	folder := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	caller := exec.Command(self, "-test.run=^TestDetachCallerProcess$")
	caller.Env = append(os.Environ(), detachTestEnv+"="+folder)
	if out, err := caller.CombinedOutput(); err != nil {
		t.Fatalf("caller: %v\n%s", err, out)
	}
	pidBytes, err := os.ReadFile(filepath.Join(folder, "supervisor.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid supervisor pid %q: %v", pidBytes, err)
	}
	if err := os.WriteFile(filepath.Join(folder, "caller-exited"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("pid=%d group=%d\nCouncil supervisor completed after caller exit\n", pid, pid)
	deadline := time.Now().Add(5 * time.Second)
	var log []byte
	for time.Now().Before(deadline) {
		log, err = os.ReadFile(filepath.Join(folder, "supervisor.log"))
		if err == nil && string(log) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("supervisor log = %q, want %q (read error: %v)", log, want, err)
}

func TestDetachLogError(t *testing.T) {
	err := Detach(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "create Council supervisor log") {
		t.Fatalf("Detach error = %v", err)
	}
}

func TestDetachPIDError(t *testing.T) {
	folder := t.TempDir()
	t.Setenv(detachTestEnv, folder)
	if err := os.Mkdir(filepath.Join(folder, "supervisor.pid"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Detach(folder)
	if err == nil || !strings.Contains(err.Error(), "write Council supervisor pid") {
		t.Fatalf("Detach error = %v", err)
	}
}
