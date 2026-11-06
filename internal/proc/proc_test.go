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

func TestCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		ok   bool
	}{
		{"exit error", &ExitError{Code: 2}, 2, true},
		{"plain error", errors.New("failed"), 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := Code(c.err); got != c.want || ok != c.ok {
				t.Errorf("Code(%v) = (%d, %t), want (%d, %t)", c.err, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestKeepAwakeSharesProcess(t *testing.T) {
	log := fakeCaffeinate(t)
	first := KeepAwake(true)
	if first == nil {
		t.Fatal("first call did not start caffeinate")
	}
	t.Cleanup(first)
	pid := caffeinatePID(t, log, 1)
	second := KeepAwake(true)
	if second == nil {
		t.Fatal("second call did not return a stop function")
	}
	t.Cleanup(second)
	first()
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("caffeinate stopped before the second stop: %v", err)
	}
	first()
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("a repeated stop ended caffeinate: %v", err)
	}
	second()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("caffeinate still exists after both stops: %v", err)
	}
	third := KeepAwake(true)
	if third == nil {
		t.Fatal("third call did not start caffeinate")
	}
	t.Cleanup(third)
	pid = caffeinatePID(t, log, 2)
	second()
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("an earlier stop ended the new caffeinate: %v", err)
	}
	third()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("new caffeinate still exists after its stop: %v", err)
	}
}

func TestKeepAwakeOff(t *testing.T) {
	log := fakeCaffeinate(t)
	if stop := KeepAwake(false); stop != nil {
		stop()
		t.Fatal("KeepAwake(false) returned a stop function")
	}
	if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("KeepAwake(false) started caffeinate: %v", err)
	}
}

func fakeCaffeinate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "starts")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"$$\" \"$*\" >> \"$KEEP_AWAKE_LOG\"\nexec /bin/sleep 60\n"
	if err := os.WriteFile(filepath.Join(dir, "caffeinate"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("KEEP_AWAKE_LOG", log)
	return log
}

func caffeinatePID(t *testing.T, log string, starts int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(log)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(data) > 0 && len(lines) >= starts {
			if len(lines) != starts {
				t.Fatalf("caffeinate starts = %d, want %d", len(lines), starts)
			}
			fields := strings.Fields(lines[starts-1])
			want := fmt.Sprintf("-i -s -w %d", os.Getpid())
			if len(fields) != 5 || strings.Join(fields[1:], " ") != want {
				t.Fatalf("caffeinate log = %q, want PID and %q", lines[starts-1], want)
			}
			pid, err := strconv.Atoi(fields[0])
			if err != nil {
				t.Fatal(err)
			}
			return pid
		}
		if time.Now().After(deadline) {
			t.Fatalf("caffeinate did not start %d times: %q", starts, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
