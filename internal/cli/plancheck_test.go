package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/proc"
	"github.com/jalendport/maestro/internal/run"
)

func TestWaitPlanCheck(t *testing.T) {
	for _, finding := range []bool{false, true} {
		t.Run(map[bool]string{false: "none", true: "finding"}[finding], func(t *testing.T) {
			_, s := waitFixture(t)
			s.State, s.PlanCheck = run.StateHeld, "running"
			s.Approval = []string{"plan check running"}
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				time.Sleep(50 * time.Millisecond)
				s.PlanCheck, s.Approval = "done", nil
				if finding {
					s.Approval = []string{"plan check: task 2: missing path; fix: correct the path"}
				} else {
					s.State = run.StateLive
				}
				if err := s.Save(); err != nil {
					done <- err
					return
				}
				if !finding {
					time.Sleep(50 * time.Millisecond)
					s.State, s.Tasks[0].State = run.StateFinished, run.TaskDone
					done <- s.Save()
				} else {
					done <- nil
				}
			}()
			output, err := waitCommand(t, "wait", s.Run)
			if saveErr := <-done; saveErr != nil {
				t.Fatal(saveErr)
			}
			if finding {
				if code, ok := proc.Code(err); !ok || code != proc.CodeOrchestrator {
					t.Fatalf("wait error: %v", err)
				}
				if !strings.Contains(output, "plan check: task 2: missing path; fix: correct the path") || !strings.Contains(output, "maestro approve "+s.Run) {
					t.Fatalf("missing findings or next command: %s", output)
				}
			} else if err != nil || !strings.Contains(output, "finished") {
				t.Fatalf("wait did not continue through Run: %v; %s", err, output)
			}
		})
	}
}

func TestWaitRecoversDeadPlanCheck(t *testing.T) {
	for _, pid := range []int{0, 1 << 30} {
		t.Run(map[bool]string{false: "dead", true: "never started"}[pid == 0], func(t *testing.T) {
			_, s := waitFixture(t)
			s.State, s.PlanCheck, s.Pid = run.StateHeld, "running", pid
			s.StartedAt = time.Now().Add(-time.Minute)
			s.Approval = []string{"plan check running", "another approval trigger"}
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				output, err := waitCommand(t, "wait", s.Run)
				if code, ok := proc.Code(err); !ok || code != proc.CodeOrchestrator {
					t.Fatalf("wait: %s, %v", output, err)
				}
				for _, want := range []string{"no result (supervisor gone)", "another approval trigger", "maestro approve " + s.Run} {
					if !strings.Contains(output, want) {
						t.Fatalf("missing %q: %s", want, output)
					}
				}
			}
			saved, err := run.Load(s.Folder)
			if err != nil || saved.PlanCheck != "done" || saved.State != run.StateHeld {
				t.Fatalf("recovered check: %+v, %v", saved, err)
			}
		})
	}
}

func TestCancelLivePlanCheckQueuesOnItsSupervisor(t *testing.T) {
	_, s := waitFixture(t)
	s.State, s.PlanCheck, s.Pid = run.StateHeld, "running", os.Getpid()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	output, err := waitCommand(t, "cancel", s.Run)
	if err != nil || !strings.Contains(output, "cancel requested") {
		t.Fatalf("cancel: %s, %v", output, err)
	}
	if _, err := os.Stat(filepath.Join(s.Folder, "control", "cancel")); err != nil {
		t.Fatal(err)
	}
}

func TestPlanCheckHelpAndFlag(t *testing.T) {
	c := (&app{}).runCmd()
	if c.Flags().Lookup("no-plan-check") == nil {
		t.Fatal("missing skip flag")
	}
	if !strings.Contains(planHelp, "Plan check") {
		t.Fatal("missing Plan check help")
	}
	if !strings.Contains(briefHelp, "For a single Dispatch, commit: true lands one commit on your branch after the gate; commit: false lands the changes uncommitted.") {
		t.Fatal("missing Dispatch commit help")
	}
}

func TestCancelPlanCheckBeforeSupervisorStarts(t *testing.T) {
	_, s := waitFixture(t)
	s.State, s.PlanCheck, s.Pid = run.StateHeld, "running", 0
	s.StartedAt = time.Now().Add(-time.Minute)
	s.PlanCheckStarted = time.Now()
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	output, err := waitCommand(t, "cancel", s.Run)
	if err != nil || !strings.Contains(output, "cancel requested") {
		t.Fatalf("cancel: %s, %v", output, err)
	}
	s.PlanCheckStarted = time.Now().Add(-time.Minute)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	output, err = waitCommand(t, "wait", s.Run)
	if code, ok := proc.Code(err); !ok || code != proc.CodeOrchestrator || !strings.Contains(output, "cancel pending") {
		t.Fatalf("wait: %s, %v", output, err)
	}
}
