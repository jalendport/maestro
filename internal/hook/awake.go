package hook

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/roster"
)

type turnAwakeRecord struct {
	PID     int    `json:"pid"`
	Started string `json:"started"`
	Event   int64  `json:"event"`
}

// A lease can end before the next event. Check the process start time so a
// reused pid in an old record cannot stop a different process.
var processStarted = readProcessStarted

func readProcessStarted(pid int) string {
	out, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// OrchestratorPID checks the same configured binaries as Dispatch.
func OrchestratorPID(r *roster.Roster, parent int, lookup func(int) (proc.Process, error)) int {
	bins := map[string]string{}
	for name, h := range r.Harness {
		bins[name] = h.Bin
	}
	reg := harness.New(bins)
	var paths []string
	for _, name := range reg.Names() {
		h, _ := reg.Get(name)
		paths = append(paths, h.Bin())
	}
	return proc.Ancestor(parent, paths, lookup)
}

// QueueAwake starts a detached worker after the Reminder is written. It does
// not wait for a control lock or a caffeinate process.
func QueueAwake(r *roster.Roster, event string) {
	queueAwake(r, event, os.Getppid(), proc.SnapshotProcess, detachAwake)
}

func queueAwake(r *roster.Roster, event string, parent int, lookup func(int) (proc.Process, error), start func(string, int, string, int64)) {
	at := time.Now().UnixNano()
	if r == nil || !r.KeepAwake() {
		return
	}
	seen := map[int]bool{}
	for parent > 1 && !seen[parent] {
		seen[parent] = true
		p, err := lookup(parent)
		if err != nil || p.Started == "" {
			logAwakeParent(parent)
			return
		}
		switch filepath.Base(p.Executable) {
		case "sh", "bash", "zsh", "dash", "ksh", "fish":
			parent = p.Parent
		default:
			start(event, parent, p.Started, at)
			return
		}
	}
}

func detachAwake(event string, pid int, started string, at int64) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "hook", "awake", event, strconv.Itoa(pid), started, strconv.FormatInt(at, 10))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}

// TurnAwake finds the Orchestrator and updates its lease in the detached worker.
func TurnAwake(r *roster.Roster, parent int, started string, event string, at int64) {
	turnAwake(r, parent, started, event, at, proc.LookupProcess)
}

func turnAwake(r *roster.Roster, parent int, started string, event string, at int64, lookup func(int) (proc.Process, error)) {
	_ = PruneAwake(config.Home())
	if r == nil || !r.KeepAwake() {
		return
	}
	p, err := lookup(parent)
	if err != nil || started == "" || p.Started != started {
		logAwakeParent(parent)
		return
	}
	pid := OrchestratorPID(r, parent, lookup)
	updateAwake(pid, event, true, at)
}

func logAwakeParent(pid int) {
	dir := filepath.Join(config.Home(), "control", "awake")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "worker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		defer f.Close()
		_, _ = f.WriteString("awake: parent " + strconv.Itoa(pid) + " is gone or its start time changed\n")
	}
}

// PruneAwake removes lease records for Orchestrator processes that have ended.
// home is the Maestro home directory. Clean can use the same lease lock.
func PruneAwake(home string) error {
	dir := filepath.Join(home, "control", "awake")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return pruneAwake(dir)
}

func pruneAwake(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || entry.IsDir() || proc.Alive(pid) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// A lock keeps concurrent events for one Orchestrator from leaving more than
// one caffeinate alive.
func updateAwake(pid int, event string, on bool, at int64) {
	if pid <= 1 || (event != "reminder" && event != "stop") {
		return
	}
	dir := filepath.Join(config.Home(), "control", "awake")
	if os.MkdirAll(dir, 0o700) != nil {
		return
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		return
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	path := filepath.Join(dir, strconv.Itoa(pid))
	// Detached workers can take the lock in a different order from the events.
	var old turnAwakeRecord
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &old)
	}
	if old.Event >= at {
		return
	}
	if old.PID > 1 && old.Started != "" && old.Started == processStarted(old.PID) {
		if syscall.Kill(old.PID, syscall.SIGKILL) != nil {
			return // Do not add a second caffeinate if the old one cannot stop.
		}
		deadline := time.Now().Add(time.Second)
		for proc.Alive(old.PID) {
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	// Keep the event time after Stop so a late worker cannot restart the turn.
	record := turnAwakeRecord{Event: at}
	save := func() bool {
		data, err := json.Marshal(record)
		return err == nil && os.WriteFile(path, data, 0o600) == nil
	}
	if !save() {
		return
	}
	if event == "stop" || !on || !proc.Alive(pid) {
		return
	}
	cmd := exec.Command("caffeinate", "-i", "-s", "-w", strconv.Itoa(pid), "-t", "1800")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() != nil {
		return
	}
	go cmd.Wait()
	record.PID, record.Started = cmd.Process.Pid, processStarted(cmd.Process.Pid)
	if record.Started == "" {
		_ = cmd.Process.Kill()
		return
	}
	if !save() {
		_ = cmd.Process.Kill()
	}
}
