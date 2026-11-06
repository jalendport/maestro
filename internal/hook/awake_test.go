package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
)

func awakeFixture(t *testing.T) (*roster.Roster, string) {
	t.Helper()
	previous := processStarted
	processStarted = func(pid int) string {
		if proc.Alive(pid) {
			return strconv.Itoa(pid)
		}
		return ""
	}
	t.Cleanup(func() { processStarted = previous })
	t.Setenv("MAESTRO_HOME", t.TempDir())
	dir := t.TempDir()
	log := filepath.Join(dir, "starts")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"$$\" \"$*\" >> \"$AWAKE_LOG\"\nexec /bin/sleep 60\n"
	if err := os.WriteFile(filepath.Join(dir, "caffeinate"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("AWAKE_LOG", log)
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	h := r.Harness["codex"]
	h.Bin = filepath.Join(dir, "custom-codex")
	r.Harness["codex"] = h
	t.Cleanup(func() { updateAwake(os.Getpid(), "stop", true, time.Now().Add(time.Hour).UnixNano()) })
	return r, log
}

func awakeLookup(r *roster.Roster) func(int) (proc.Process, error) {
	return func(pid int) (proc.Process, error) {
		if pid == 99 {
			return proc.Process{Parent: os.Getpid(), Executable: "/bin/sh", Started: "shell-start"}, nil
		}
		return proc.Process{Parent: 1, Executable: r.Harness["codex"].Bin, Started: "harness-start"}, nil
	}
}

func awakeRecord(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(config.Home(), "control", "awake", strconv.Itoa(os.Getpid())))
	if err != nil {
		t.Fatal(err)
	}
	var record turnAwakeRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	return record.PID
}

func waitAwakeState(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("sleep control did not reach the expected state")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func awakeStarts(t *testing.T, path string, count int) {
	t.Helper()
	waitAwakeState(t, func() bool {
		data, _ := os.ReadFile(path)
		return strings.Count(string(data), "\n") >= count
	})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != count {
		t.Fatalf("starts = %q, want %d", data, count)
	}
	for _, line := range lines {
		_, args, _ := strings.Cut(line, " ")
		if want := fmt.Sprintf("-i -s -w %d -t 1800", os.Getpid()); args != want {
			t.Fatalf("arguments = %q, want %q", args, want)
		}
	}
}

func TestReminderAwakeRefreshAndStop(t *testing.T) {
	r, log := awakeFixture(t)
	start := func(event string, parent int, started string, at int64) {
		turnAwake(r, parent, started, event, at, awakeLookup(r))
	}
	queueAwake(r, "reminder", 99, awakeLookup(r), start)
	awakeStarts(t, log, 1)
	first := awakeRecord(t)
	queueAwake(r, "reminder", 99, awakeLookup(r), start)
	awakeStarts(t, log, 2)
	second := awakeRecord(t)
	if first == second {
		t.Fatal("Reminder did not refresh the lease")
	}
	waitAwakeState(t, func() bool { return !proc.Alive(first) })
	if !proc.Alive(second) {
		t.Fatal("refreshed caffeinate is gone")
	}
	// The Stop payload is parsed through the same path as the installed hook.
	call, err := Parse("claude", []byte(`{"hook_event_name":"Stop"}`))
	if err != nil || call.Event != "stop" {
		t.Fatalf("Stop = %+v, %v", call, err)
	}
	queueAwake(r, call.Event, 99, awakeLookup(r), start)
	waitAwakeState(t, func() bool { return !proc.Alive(second) })
	if got := awakeRecord(t); got != 0 {
		t.Fatalf("Stop kept a caffeinate pid: %d", got)
	}
}

func TestReminderAwakeOffAndNoAncestor(t *testing.T) {
	r, log := awakeFixture(t)
	off := false
	r.Failure.KeepAwake = &off
	called := false
	start := func(string, int, string, int64) { called = true }
	queueAwake(r, "reminder", 99, awakeLookup(r), start)
	if called {
		t.Fatal("keep_awake=false queued work")
	}
	r.Failure.KeepAwake = nil
	turnAwake(r, 99, "other-start", "reminder", time.Now().UnixNano(), func(int) (proc.Process, error) {
		return proc.Process{Parent: 1, Executable: "/other/program", Started: "other-start"}, nil
	})
	updateAwake(os.Getpid(), "reminder", false, time.Now().UnixNano())
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("started caffeinate: %v", err)
	}
}

func TestQueueAwakeResolvesShellBeforeWorker(t *testing.T) {
	r, log := awakeFixture(t)
	var event string
	var parent int
	var started string
	var at int64
	queueAwake(r, "reminder", 99, awakeLookup(r), func(e string, p int, s string, timestamp int64) {
		event, parent, started, at = e, p, s, timestamp
	})
	if parent != os.Getpid() || started != "harness-start" || event != "reminder" || at == 0 {
		t.Fatalf("worker arguments = %s, %d, %d", event, parent, at)
	}
	if _, err := os.Stat(filepath.Join(config.Home(), "control", "awake")); !os.IsNotExist(err) {
		t.Fatalf("hook wrote lease state before the worker: %v", err)
	}
	lookups := 0
	turnAwake(r, parent, started, event, at, func(pid int) (proc.Process, error) {
		lookups++
		if pid == 99 {
			return proc.Process{}, fmt.Errorf("shell is gone")
		}
		return awakeLookup(r)(pid)
	})
	if lookups != 2 {
		t.Fatalf("worker process lookups = %d, want 2", lookups)
	}
	awakeStarts(t, log, 1)
}

func TestTurnAwakeRejectsChangedOrMissingParent(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(strconv.FormatBool(missing), func(t *testing.T) {
			r, log := awakeFixture(t)
			turnAwake(r, 99, "old-start", "reminder", time.Now().UnixNano(), func(int) (proc.Process, error) {
				if missing {
					return proc.Process{}, fmt.Errorf("parent is gone")
				}
				return proc.Process{Parent: 1, Executable: r.Harness["codex"].Bin, Started: "new-start"}, nil
			})
			dir := filepath.Join(config.Home(), "control", "awake")
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "worker.log" {
				t.Fatalf("worker wrote a lease record: %v", entries)
			}
			data, err := os.ReadFile(filepath.Join(dir, "worker.log"))
			if err != nil || strings.Count(string(data), "\n") != 1 || !strings.Contains(string(data), "parent 99") {
				t.Fatalf("worker log = %q, %v", data, err)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("worker started caffeinate: %v", err)
			}
		})
	}
}

func TestQueueAwakeMakesNoPSCall(t *testing.T) {
	r, _ := awakeFixture(t)
	processStarted = func(int) string {
		t.Fatal("hook called the ps start-time reader")
		return ""
	}
	want, err := proc.SnapshotProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	called := false
	queueAwake(r, "reminder", os.Getpid(), proc.SnapshotProcess, func(event string, pid int, started string, at int64) {
		called = true
		if pid != os.Getpid() || started != want.Started || event != "reminder" || at == 0 {
			t.Fatalf("worker identity = %d, %q, %s, %d", pid, started, event, at)
		}
	})
	if !called {
		t.Fatal("hook did not queue the worker")
	}
}

func TestTurnAwakePrunesDeadOrchestratorRecords(t *testing.T) {
	r, _ := awakeFixture(t)
	dead := exec.Command("/bin/sleep", "60")
	if err := dead.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dead.Process.Kill() })
	if err := dead.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = dead.Wait()
	dir := filepath.Join(config.Home(), "control", "awake")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(dir, strconv.Itoa(os.Getpid()))
	deadPath := filepath.Join(dir, strconv.Itoa(dead.Process.Pid))
	for _, path := range []string{livePath, deadPath} {
		if err := os.WriteFile(path, []byte(`{"event":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Prune even when no ancestor matches this worker's parent.
	turnAwake(r, 99, "other-start", "stop", time.Now().UnixNano(), func(int) (proc.Process, error) {
		return proc.Process{Parent: 1, Executable: "/other/program", Started: "other-start"}, nil
	})
	if _, err := os.Stat(deadPath); !os.IsNotExist(err) {
		t.Fatalf("dead Orchestrator record remains: %v", err)
	}
	if data, err := os.ReadFile(livePath); err != nil || string(data) != `{"event":1}` {
		t.Fatalf("live Orchestrator record changed: %s, %v", data, err)
	}
}

func TestTurnAwakeReplacesDeadAndOrdersWorkers(t *testing.T) {
	_, log := awakeFixture(t)
	pid := os.Getpid()
	firstAt := time.Now().UnixNano()
	updateAwake(pid, "reminder", true, firstAt)
	awakeStarts(t, log, 1)
	old := awakeRecord(t)
	p, err := os.FindProcess(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	waitAwakeState(t, func() bool { return !proc.Alive(old) })
	updateAwake(pid, "reminder", true, firstAt+1)
	awakeStarts(t, log, 2)
	current := awakeRecord(t)
	updateAwake(pid, "stop", true, firstAt+3)
	updateAwake(pid, "reminder", true, firstAt+2)
	waitAwakeState(t, func() bool { return !proc.Alive(current) })
	if got := awakeRecord(t); got != 0 {
		t.Fatalf("late worker restarted a finished turn: %d", got)
	}
}

func TestTurnAwakeConcurrentEventsKeepOneProcess(t *testing.T) {
	_, log := awakeFixture(t)
	pid := os.Getpid()
	var workers sync.WaitGroup
	at := time.Now().UnixNano()
	for i := range 8 {
		workers.Add(1)
		go func() { defer workers.Done(); updateAwake(pid, "reminder", true, at+int64(i)) }()
	}
	workers.Wait()
	if !proc.Alive(awakeRecord(t)) {
		t.Fatal("no live caffeinate after concurrent events")
	}
	current := awakeRecord(t)
	waitAwakeState(t, func() bool {
		data, _ := os.ReadFile(log)
		return strings.Contains(string(data), strconv.Itoa(current)+" ")
	})
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		fields := strings.Fields(line)
		old, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatal(err)
		}
		if old != current && proc.Alive(old) {
			t.Fatalf("second caffeinate %d is still alive", old)
		}
	}
}

func TestTurnAwakeDoesNotKillReusedPID(t *testing.T) {
	_, _ = awakeFixture(t)
	other := exec.Command("/bin/sleep", "60")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
	dir := filepath.Join(config.Home(), "control", "awake")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(turnAwakeRecord{PID: other.Process.Pid, Started: "an earlier process"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(os.Getpid())), data, 0o600); err != nil {
		t.Fatal(err)
	}
	updateAwake(os.Getpid(), "stop", true, time.Now().UnixNano())
	if !proc.Alive(other.Process.Pid) {
		t.Fatal("stale record stopped a different process")
	}
}
