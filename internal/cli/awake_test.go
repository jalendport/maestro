package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/run"
)

func TestWaitKeepsAwakeUntilReturn(t *testing.T) {
	for _, kind := range []string{"dispatch", "run", "council"} {
		t.Run(kind, func(t *testing.T) {
			rec, state := waitFixture(t)
			dir := t.TempDir()
			log := filepath.Join(dir, "starts")
			script := "#!/bin/sh\nprintf '%s %s\\n' \"$$\" \"$*\" > \"$WAIT_AWAKE_LOG\"\nexec /bin/sleep 60\n"
			if err := os.WriteFile(filepath.Join(dir, "caffeinate"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("WAIT_AWAKE_LOG", log)
			id := rec.Task
			finish := func() error {
				rec.State, rec.Outcome.Outcome = "ended", dispatch.OutcomeAnswered
				return rec.Save()
			}
			switch kind {
			case "run":
				id = state.Run
				if err := state.Save(); err != nil {
					t.Fatal(err)
				}
				finish = func() error { state.State = run.StateFinished; return state.Save() }
			case "council":
				id = "awake-council"
				c := &council.Council{Project: rec.Project, Slug: id, Path: filepath.Join(config.CouncilsDir(rec.Project), id), State: "running", Pid: os.Getpid()}
				if err := os.MkdirAll(c.Path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := c.Save(); err != nil {
					t.Fatal(err)
				}
				finish = func() error { c.State = "finished"; return c.Save() }
			}
			type result struct {
				pid int
				err error
			}
			done := make(chan result, 1)
			go func() {
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					data, _ := os.ReadFile(log)
					fields := strings.Fields(string(data))
					if len(fields) == 5 {
						pid, err := strconv.Atoi(fields[0])
						want := fmt.Sprintf("-i -s -w %d", os.Getpid())
						if err == nil && (!proc.Alive(pid) || strings.Join(fields[1:], " ") != want) {
							err = fmt.Errorf("caffeinate = %q", data)
						}
						finishErr := finish()
						if err == nil {
							err = finishErr
						}
						done <- result{pid: pid, err: err}
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				_ = finish()
				done <- result{err: fmt.Errorf("wait did not start caffeinate")}
			}()
			_, err := waitCommand(t, "wait", id, "--json")
			got := <-done
			if err != nil || got.err != nil {
				t.Fatalf("wait = %v; caffeinate = %v", err, got.err)
			}
			if proc.Alive(got.pid) {
				t.Fatal("wait kept caffeinate after return")
			}
		})
	}
}

func TestWaitAwakeOff(t *testing.T) {
	rec, _ := waitFixture(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "unexpected")
	if err := os.WriteFile(filepath.Join(dir, "caffeinate"), []byte("#!/bin/sh\nprintf started > \"$WAIT_AWAKE_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAIT_AWAKE_LOG", log)
	if err := os.WriteFile(config.UserFile(), []byte("[failure]\nkeep_awake = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		rec.State, rec.Outcome.Outcome = "ended", dispatch.OutcomeAnswered
		done <- rec.Save()
	}()
	_, err := waitCommand(t, "wait", rec.Task, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("keep_awake=false started caffeinate: %v", err)
	}
}
