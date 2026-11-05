package run

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResumeSavedBeforeMarkerRemoved(t *testing.T) {
	env := quietRunEnv(t)
	s := saveRun(t, env, &TaskState{Number: 1, ID: "abcd", State: TaskBlocked})
	path := resumeFile(t, s.Folder, Resume{Task: "abcd", Lane: "codex-sol"})
	sup := &Supervisor{Env: env, State: s}
	sup.consumeControl()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("resume marker: %v", err)
	}
	current, err := Load(s.Folder)
	if err != nil || current.Tasks[0].State != TaskWaiting || ResumeTask(current) != nil {
		t.Fatalf("answer is not saved: %+v, %v", current, err)
	}
	if got := ReminderItems(env.Root); strings.Contains(got, "blocked") {
		t.Fatalf("answered Task in Reminder: %s", got)
	}
}

func TestResumeSaveFailureKeepsMarker(t *testing.T) {
	env := quietRunEnv(t)
	s := saveRun(t, env, &TaskState{Number: 1, ID: "abcd", State: TaskBlocked})
	path := resumeFile(t, s.Folder, Resume{Task: "abcd", Skip: true})
	if err := os.Mkdir(filepath.Join(s.Folder, FileState+".tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{Env: env, State: s}
	sup.consumeControl()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unsaved answer was removed: %v", err)
	}
}

func TestResumeDoesNotRestartRunningTask(t *testing.T) {
	env := quietRunEnv(t)
	s := saveRun(t, env, &TaskState{Number: 1, ID: "abcd", State: TaskRunning})
	sup := &Supervisor{Env: env, State: s}
	sup.consumeControl()
	resumeFile(t, s.Folder, Resume{Task: "abcd", Lane: "codex-sol"})
	sup.consumeControl()
	if s.Tasks[0].State != TaskRunning {
		t.Fatalf("second answer restarted Task: %+v", s.Tasks[0])
	}
}

func TestStartRefusesLiveOwner(t *testing.T) {
	env := quietRunEnv(t)
	s := saveRun(t, env, &TaskState{Number: 1, ID: "abcd", State: TaskBlocked})
	if err := WriteIndex(s); err != nil {
		t.Fatal(err)
	}
	// A new owner can be indexed before it saves its PID over a dead owner.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(self, "-test.run=^$")
	if err := child.Run(); err != nil {
		t.Fatal(err)
	}
	s.Pid = child.Process.Pid
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { RemoveIndex(s.Project, s.Run) })
	if err := Start(env, s.Folder); err == nil || !strings.Contains(err.Error(), "supervisor") {
		t.Fatalf("second supervisor was accepted: %v", err)
	}
	if _, live := LiveRunFor(s.Project); !live {
		t.Fatal("second supervisor removed the owner's index")
	}
}

func TestPauseOwnershipHandoff(t *testing.T) {
	for _, beforeRelease := range []bool{true, false} {
		t.Run(map[bool]string{true: "answer before release", false: "answer after release"}[beforeRelease], func(t *testing.T) {
			env := quietRunEnv(t)
			s := saveRun(t, env, &TaskState{Number: 1, ID: "abcd", State: TaskBlocked})
			sup := &Supervisor{Env: env, State: s, ownsRun: true}
			if err := WriteIndex(s); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { RemoveIndex(s.Project, s.Run) })
			if err := sup.end(StatePaused); err != nil {
				t.Fatal(err)
			}
			if !SupervisorAlive(s) {
				t.Fatal("pause released ownership before the decision")
			}
			answer := Resume{Task: "abcd", Skip: true}
			if beforeRelease {
				if live, err := QueueResume(s.Folder, answer); err != nil || !live {
					t.Fatalf("answer was not given to the owner: live=%v, %v", live, err)
				}
			}
			resumed, err := sup.finishOwnership(StatePaused)
			if err != nil || resumed != beforeRelease || sup.ownsRun != beforeRelease {
				t.Fatalf("ownership decision: resumed=%v, owns=%v, %v", resumed, sup.ownsRun, err)
			}
			if beforeRelease {
				current, err := Load(s.Folder)
				if err != nil || current.State != StateLive || ResumeTask(current) != nil || PendingResume(s.Folder, "") {
					t.Fatalf("answered pause is still visible: %+v, %v", current, err)
				}
				if !SupervisorAlive(current) {
					t.Fatal("resumed owner lost its index")
				}
			} else {
				if live, err := QueueResume(s.Folder, answer); err != nil || live {
					t.Fatalf("answer was given to the old owner: live=%v, %v", live, err)
				}
				if err := Start(env, s.Folder); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
