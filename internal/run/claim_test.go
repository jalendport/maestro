package run

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestSchedulingFullAccountDoesNoPreparation(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "fixture-key")
	var routes atomic.Int32
	takeSlot := make(chan func() error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		routes.Add(1)
		select {
		case claim := <-takeSlot:
			if err := claim(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		default:
		}
		http.Error(w, "Router must not run while the Task waits", http.StatusBadRequest)
	}))
	defer server.Close()
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	r.Router.Backend, r.Router.BaseURL = "jev", server.URL
	r.Concurrency = config.Concurrency{MaxParallel: 4, MaxTotal: 6, MaxPerAccount: 1}
	off := false
	r.Failure.KeepAwake = &off
	r.Families = map[string]*roster.Family{"codex-sol": r.Families["codex-sol"]}
	r.Accounts = map[string]config.Account{"codex": r.Accounts["codex"]}
	busy := claimSupervisor(t, r, runningOnAccount(1, "codex")...)
	task := &TaskState{ID: "next", State: TaskWaiting, Access: brief.AccessEdit, Provisional: true, Route: &router.Route{Account: "codex"}}
	sup := claimSupervisor(t, r, task)
	sup.results = make(chan result, 1)
	sup.errorTexts = map[string]int{}
	task.Folder = filepath.Join(sup.State.Folder, task.ID)
	task.BriefPath = filepath.Join(sup.Env.Root, "source.md")
	b := &brief.Brief{Fields: brief.Fields{Title: "Wait for the Account", Access: brief.AccessEdit, Category: "implement", Level: "some"}, Goal: "Check the slot.", Done: "Return a Report."}
	if err := brief.Write(b, task.BriefPath); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	snapshots := filepath.Join(t.TempDir(), "snapshots")
	probes := filepath.Join(t.TempDir(), "probes")
	t.Setenv("MAESTRO_TEST_SNAPSHOTS", snapshots)
	t.Setenv("MAESTRO_TEST_PROBES", probes)
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte("#!/bin/sh\nprintf 'snapshot\\n' >> \"$MAESTRO_TEST_SNAPSHOTS\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ -z \"$MAESTRO_TASK_FOLDER\" ]; then printf 'probe\\n' >> \"$MAESTRO_TEST_PROBES\"; exit 1; fi\nread pointer\nprintf 'STATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	sup.Env.Reg = harness.New(map[string]string{"codex": bin})
	sup.Env.Disc = &discovery.Cache{Harness: map[string]discovery.Record{"codex": {Path: bin, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}}}}
	sup.Env.Budget = &budget.File{Accounts: map[string]*budget.Account{}}
	for range 25 {
		sup.schedule()
		if task.State != TaskWaiting || task.Waiting != "account cap codex" || task.Escalations != 0 {
			t.Fatalf("waiting Task changed: %+v", task)
		}
	}
	if routes.Load() != 0 {
		t.Fatalf("Router calls while waiting: %d", routes.Load())
	}
	for _, path := range []string{snapshots, probes, filepath.Join(task.Folder, dispatch.FileTask)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("preparation while waiting at %s: %v", path, err)
		}
	}
	// An unreadable Brief must also wait, without an escalation.
	path := task.BriefPath
	task.BriefPath = filepath.Join(sup.Env.Root, "missing.md")
	sup.schedule()
	if task.State != TaskWaiting || task.Escalations != 0 {
		t.Fatalf("wait escalated: %+v", task)
	}
	task.BriefPath = path
	RemoveIndex(busy.State.Project, busy.State.Run)
	sup.Env.Budget = nil
	// A second supervisor takes the slot during the Router call.
	takeSlot <- func() error { return WriteIndex(busy.State) }
	sup.schedule()
	if task.State != TaskWaiting || task.Waiting != "account cap codex" || len(sup.pending) != 1 {
		t.Fatalf("lost slot did not keep the Route: %+v", task)
	}
	for range 25 {
		sup.schedule()
	}
	if routes.Load() != 1 {
		t.Fatalf("lost slot repeated the Router call: %d", routes.Load())
	}
	for _, path := range []string{snapshots, probes, filepath.Join(task.Folder, dispatch.FileTask)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("preparation after a lost slot at %s: %v", path, err)
		}
	}
	RemoveIndex(busy.State.Project, busy.State.Run)
	r.Router.Backend = "rules"
	sup.schedule()
	if task.State != TaskRunning {
		t.Fatalf("free slot did not start Task: %+v", task)
	}
	select {
	case got := <-sup.results:
		if got.err != nil || got.rec.Outcome.Outcome != dispatch.OutcomeUnverified {
			t.Fatalf("Dispatch = %+v, %v", got.rec, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Task did not finish after its slot became free")
	}
}

func claimSupervisor(t *testing.T, r *roster.Roster, tasks ...*TaskState) *Supervisor {
	t.Helper()
	env := &dispatch.Env{Root: t.TempDir(), Roster: r}
	s := saveRun(t, env, tasks...)
	if err := WriteIndex(s); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{Env: env, State: s, running: map[string]chan struct{}{}}
	for _, task := range tasks {
		if task.State == TaskRunning {
			sup.running[task.ID] = make(chan struct{})
		}
	}
	return sup
}

func runningOnAccount(n int, account string) []*TaskState {
	tasks := make([]*TaskState, n)
	for i := range tasks {
		tasks[i] = &TaskState{ID: fmt.Sprint(i), State: TaskRunning, Route: &router.Route{Account: account}}
	}
	return tasks
}

func TestClaimAccountCapFromUserFile(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			user := "[concurrency]\nmax_total = 4\n"
			if explicit {
				user += "max_per_account = 2\n"
			}
			if err := os.WriteFile(config.UserFile(), []byte(user), 0o644); err != nil {
				t.Fatal(err)
			}
			r, err := roster.Load()
			if err != nil {
				t.Fatal(err)
			}
			n := 3
			if explicit {
				n = 2
			}
			claimSupervisor(t, r, runningOnAccount(n, "codex")...)
			task := &TaskState{ID: "next", State: TaskWaiting}
			sup := claimSupervisor(t, r, task)
			claimed, err := sup.claim(task, &router.Route{Account: "codex"})
			if err != nil {
				t.Fatal(err)
			}
			if explicit {
				if claimed || task.Waiting != "account cap codex" || task.State != TaskWaiting {
					t.Fatalf("set Account cap: claimed=%v Task=%+v", claimed, task)
				}
				return
			}
			if !claimed || task.Waiting != "" || task.State != TaskRunning {
				t.Fatalf("absent Account cap: claimed=%v Task=%+v", claimed, task)
			}
			sup.running[task.ID] = make(chan struct{})
			next := &TaskState{ID: "waiting", State: TaskWaiting}
			sup.State.Tasks = append(sup.State.Tasks, next)
			claimed, err = sup.claim(next, &router.Route{Account: "codex"})
			if err != nil || claimed || next.Waiting != "machine cap" {
				t.Fatalf("machine cap: claimed=%v Task=%+v error=%v", claimed, next, err)
			}
		})
	}
}

func TestClaimLastSlotAcrossRuns(t *testing.T) {
	for _, cap := range []string{"machine", "account"} {
		t.Run(cap, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			r := &roster.Roster{Concurrency: config.Concurrency{MaxTotal: 2}}
			wantWaiting := "machine cap"
			if cap == "account" {
				r.Concurrency.MaxTotal = 6
				r.Concurrency.MaxPerAccount = 2
				wantWaiting = "account cap codex"
			}
			claimSupervisor(t, r, runningOnAccount(1, "codex")...)
			var supervisors []*Supervisor
			for i := 0; i < 2; i++ {
				task := &TaskState{ID: "next", State: TaskWaiting, Provisional: true, Route: &router.Route{Account: "claude"}}
				supervisors = append(supervisors, claimSupervisor(t, r, task))
			}
			ready := sync.WaitGroup{}
			ready.Add(len(supervisors))
			start := make(chan struct{})
			type answer struct {
				claimed bool
				err     error
			}
			answers := make(chan answer, len(supervisors))
			for _, sup := range supervisors {
				go func() {
					ready.Done()
					<-start
					claimed, err := sup.claim(sup.State.Tasks[0], &router.Route{Account: "codex"})
					answers <- answer{claimed, err}
				}()
			}
			ready.Wait()
			close(start)
			claimed := 0
			for range supervisors {
				a := <-answers
				if a.err != nil {
					t.Fatal(a.err)
				}
				if a.claimed {
					claimed++
				}
			}
			if claimed != 1 {
				t.Fatalf("%d supervisors took the last slot", claimed)
			}
			for _, sup := range supervisors {
				saved, err := Load(sup.State.Folder)
				if err != nil {
					t.Fatal(err)
				}
				task := sup.State.Tasks[0]
				if task.State == TaskRunning {
					if saved.Tasks[0].State != TaskRunning || saved.Tasks[0].Route.Account != "codex" || saved.Tasks[0].Provisional {
						t.Fatalf("claim was not saved with the Dispatch Route: %+v", saved.Tasks[0])
					}
				} else if task.State != TaskWaiting || task.Waiting != wantWaiting {
					t.Fatalf("waiting Task: %+v", task)
				}
			}
		})
	}
}

func TestClaimIgnoresDeadSupervisor(t *testing.T) {
	for _, cap := range []string{"machine", "account"} {
		t.Run(cap, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			r := &roster.Roster{Concurrency: config.Concurrency{MaxTotal: 1}}
			if cap == "account" {
				r.Concurrency.MaxTotal = 6
				r.Concurrency.MaxPerAccount = 1
			}
			dead := claimSupervisor(t, r, runningOnAccount(1, "codex")...)
			cmd := exec.Command("/bin/sh", "-c", "exit 0")
			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
			idx := Index{Run: dead.State.Run, Project: dead.State.Project, Folder: dead.State.Folder, Pid: cmd.Process.Pid}
			data, err := json.Marshal(idx)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(indexPath(idx.Project, idx.Run), data, 0o644); err != nil {
				t.Fatal(err)
			}
			task := &TaskState{ID: "next", State: TaskWaiting}
			sup := claimSupervisor(t, r, task)
			if claimed, err := sup.claim(task, &router.Route{Account: "codex"}); err != nil || !claimed {
				t.Fatalf("dead supervisor kept its slot: claimed=%v error=%v", claimed, err)
			}
			if _, err := os.Stat(indexPath(idx.Project, idx.Run)); !os.IsNotExist(err) {
				t.Fatalf("dead index remains: %v", err)
			}
		})
	}
}

func TestClaimReleasesHomeLockBeforeDispatch(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	r := &roster.Roster{Concurrency: config.Concurrency{MaxTotal: 1}}
	task := &TaskState{ID: "next", State: TaskWaiting}
	sup := claimSupervisor(t, r, task)
	if claimed, err := sup.claim(task, &router.Route{Account: "codex"}); err != nil || !claimed {
		t.Fatalf("claim: %v, %v", claimed, err)
	}
	lock, err := os.OpenFile(filepath.Join(config.Home(), "concurrency.lock"), os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("claim still holds the home lock: %v", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
}

func TestClaimSaveFailureDoesNotTakeSlot(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	r := &roster.Roster{Concurrency: config.Concurrency{MaxTotal: 1}}
	task := &TaskState{ID: "next", State: TaskWaiting, Provisional: true, Route: &router.Route{Account: "claude"}}
	sup := claimSupervisor(t, r, task)
	if err := os.Mkdir(filepath.Join(sup.State.Folder, FileState+".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if claimed, err := sup.claim(task, &router.Route{Account: "codex"}); err == nil || claimed {
		t.Fatalf("claim after save failure: %v, %v", claimed, err)
	}
	if task.State != TaskWaiting || task.Route.Account != "claude" || !task.Provisional || !task.StartedAt.IsZero() {
		t.Fatalf("failed claim changed the Task: %+v", task)
	}
	saved, err := Load(sup.State.Folder)
	if err != nil || saved.Tasks[0].State != TaskWaiting {
		t.Fatalf("failed claim changed the saved Run: %+v, %v", saved, err)
	}
}
