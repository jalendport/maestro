package cli

import (
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
