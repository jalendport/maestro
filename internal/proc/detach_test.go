package proc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("MAESTRO_DETACH_TEST") != "" && len(os.Args) == 3 {
		group, err := syscall.Getpgid(0)
		if err != nil {
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "%d %d\n", os.Getpid(), group)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDetachLogAndPID(t *testing.T) {
	t.Setenv("MAESTRO_DETACH_TEST", "1")
	for _, tc := range []struct {
		name      string
		verb      string
		appendLog bool
		writePID  bool
	}{
		{"council", "_council", false, true},
		{"dispatch", "_supervise", false, true},
		{"run", "_run", true, false},
		{"append with pid", "_run", true, true},
		{"replace without pid", "_council", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folder := t.TempDir()
			logPath := filepath.Join(folder, "supervisor.log")
			if err := os.WriteFile(logPath, []byte("previous\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Detach(folder, tc.verb, tc.appendLog, tc.writePID); err != nil {
				t.Fatal(err)
			}
			var pid, group int
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				log, err := os.ReadFile(logPath)
				if err != nil {
					t.Fatal(err)
				}
				text := string(log)
				if tc.appendLog {
					if !strings.HasPrefix(text, "previous\n") {
						t.Fatalf("supervisor log lost its prior content: %q", text)
					}
					text = strings.TrimPrefix(text, "previous\n")
				} else if strings.HasPrefix(text, "previous\n") {
					t.Fatalf("supervisor log kept its prior content: %q", text)
				}
				if n, _ := fmt.Sscanf(text, "%d %d", &pid, &group); n == 2 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid <= 0 || group != pid {
				t.Fatalf("supervisor pid=%d group=%d", pid, group)
			}
			data, err := os.ReadFile(filepath.Join(folder, "supervisor.pid"))
			if !tc.writePID {
				if !os.IsNotExist(err) {
					t.Fatalf("unexpected supervisor pid file: %q, %v", data, err)
				}
			} else if err != nil || string(data) != strconv.Itoa(pid) {
				t.Fatalf("supervisor pid file: %q, %v", data, err)
			}
		})
	}
}

func TestDetachRejectsPIDWriteFailure(t *testing.T) {
	t.Setenv("MAESTRO_DETACH_TEST", "1")
	for _, verb := range []string{"_council", "_supervise"} {
		t.Run(verb, func(t *testing.T) {
			folder := t.TempDir()
			if err := os.Mkdir(filepath.Join(folder, "supervisor.pid"), 0o755); err != nil {
				t.Fatal(err)
			}
			var failure *os.PathError
			if err := Detach(folder, verb, false, true); !errors.As(err, &failure) || !strings.HasPrefix(err.Error(), "write supervisor pid: ") || failure.Path != filepath.Join(folder, "supervisor.pid") {
				t.Fatalf("pid write error = %v", err)
			}
		})
	}
}
