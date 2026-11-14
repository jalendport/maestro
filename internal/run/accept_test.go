package run

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/worktree"
)

func acceptanceFixture(t *testing.T, mode string, commit bool, state string) (*Supervisor, *dispatch.Record) {
	t.Helper()
	sup := worktreeSupervisor(t)
	sup.State.Mode = mode
	sup.State.State = StatePaused
	task := sup.State.Tasks[0]
	b := sup.pending[task].brief
	b.Commit = &commit
	if err := brief.Write(b, task.BriefPath); err != nil {
		t.Fatal(err)
	}
	root := sup.State.Project
	if mode == "worktree" {
		if err := sup.taskWorkdir(task, b); err != nil {
			t.Fatal(err)
		}
		root = task.Workdir
	}
	base := runGit(t, root, "rev-parse", "HEAD")
	runWrite(t, filepath.Join(root, "first.txt"), "accepted change\n")
	runWrite(t, filepath.Join(task.Folder, dispatch.FileReport), "## Summary\nKeep the judged change.\n\nSTATUS: done\n")
	task.State, task.Attempt, task.Outcome, task.Status = state, 1, dispatch.OutcomeFailed, brief.StatusDone
	task.Error, task.Cause = "gate failed", dispatch.CauseNone
	rec := &dispatch.Record{
		Kind: "dispatch", Project: sup.State.Project, Workdir: task.Workdir,
		Run: sup.State.Run, Task: task.ID, Path: task.Folder, State: "ended", Attempt: 1, Route: task.Route,
		TaskInfo:     dispatch.TaskInfo{Title: task.Title, Access: task.Access},
		Outcome:      dispatch.Outcome{Outcome: dispatch.OutcomeFailed, Status: brief.StatusDone, FailureClass: dispatch.FailBad, Decision: "blocked", ErrorText: "gate failed"},
		Verification: dispatch.Verification{StartCommit: base, Changed: []string{"first.txt"}, Gate: json.RawMessage(`{"cause":"none","scored":true,"verdict":"fail"}`)},
	}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Append(rec); err != nil {
		t.Fatal(err)
	}
	sup.updateDependencies()
	if err := sup.State.Save(); err != nil {
		t.Fatal(err)
	}
	return sup, rec
}

func TestAcceptCommitsByModeAndBrief(t *testing.T) {
	for _, mode := range []string{"tree", "worktree"} {
		for _, commit := range []bool{false, true} {
			for _, state := range []string{TaskBlocked, TaskFailed} {
				t.Run(fmt.Sprintf("%s/commit=%t/%s", mode, commit, state), func(t *testing.T) {
					sup, original := acceptanceFixture(t, mode, commit, state)
					sup.State.Tasks = sup.State.Tasks[:1]
					task := sup.State.Tasks[0]
					template := "{title}\n\n{summary}"
					sup.Env.Project = &config.Project{CommitMessage: &template}
					runWrite(t, filepath.Join(sup.State.Project, "user.txt"), "user work\n")
					if err := sup.State.Save(); err != nil {
						t.Fatal(err)
					}
					if live, err := QueueResume(sup.State.Folder, Resume{Task: task.ID, Accept: true}); err != nil || live {
						t.Fatalf("queue acceptance: live=%t, %v", live, err)
					}
					sup.consumeControl()
					saved, err := Load(sup.State.Folder)
					if err != nil || saved.Tasks[0].State != TaskDone || saved.Tasks[0].Decision != "accepted" || saved.State != StateLive || PendingResume(sup.State.Folder, "") {
						t.Fatalf("saved acceptance: %+v, %v", saved, err)
					}
					if task.Error != "" || task.Cause != "" || task.Waiting != "" {
						t.Fatalf("accepted Task still blocked: %+v", task)
					}
					rec, err := dispatch.LoadRecord(task.Folder)
					var gate dispatch.Gate
					if err != nil || rec.Outcome.Decision != "accepted" || rec.Outcome.Outcome != original.Outcome.Outcome || json.Unmarshal(rec.Verification.Gate, &gate) != nil || gate.Cause != dispatch.CauseNone || gate.Verdict != "fail" {
						t.Fatalf("acceptance changed the gate result: %+v, %v", rec, err)
					}
					if (rec.Verification.CommitRange != "") != (commit || mode == "worktree") {
						t.Fatalf("commit: %+v", rec.Verification)
					}
					if rec.Verification.CommitRange != "" {
						if message := runGit(t, sup.State.Project, "show", "-s", "--format=%B", rec.Verification.CommitRange); message != task.Title+"\n\nKeep the judged change." {
							t.Fatalf("commit template: %q", message)
						}
						if files := runGit(t, sup.State.Project, "show", "--format=", "--name-only", rec.Verification.CommitRange); files != "first.txt" {
							t.Fatalf("committed unrelated paths: %s", files)
						}
					}
					if mode == "tree" && !commit && !strings.Contains(runGit(t, sup.State.Project, "status", "--porcelain"), "?? first.txt") {
						t.Fatal("acceptance did not leave changes uncommitted")
					}
					if mode == "worktree" {
						if content := runGit(t, sup.State.Project, "show", sup.State.RunBranch+":first.txt"); content != "accepted change" {
							t.Fatalf("Task did not merge: %s", content)
						}
					}
					for _, view := range []string{Status(saved), Report(saved), dispatch.Status(rec)} {
						if !strings.Contains(view, "accepted") {
							t.Fatalf("missing acceptance: %s", view)
						}
					}
					rows := 0
					if err := ledger.Each(func(raw json.RawMessage) error {
						var row dispatch.Record
						if err := json.Unmarshal(raw, &row); err != nil {
							return err
						}
						rows++
						if rows == 1 && (row.Kind != "dispatch" || row.Outcome.Decision != "blocked") || rows == 2 && (row.Kind != "acceptance" || row.Outcome.Decision != "accepted" || row.Task != task.ID) {
							t.Fatalf("Ledger row %d: %s", rows, raw)
						}
						return nil
					}); err != nil || rows != 2 {
						t.Fatalf("Ledger: rows=%d, %v", rows, err)
					}
					if row := ledgerRow(saved)["tasks"].([]map[string]any)[0]; row["decision"] != "accepted" {
						t.Fatalf("Run Ledger row: %+v", row)
					}
				})
			}
		}
	}
}

func TestAcceptStartsDependentTask(t *testing.T) {
	for _, mode := range []string{"tree", "worktree"} {
		t.Run(mode, func(t *testing.T) {
			sup, _ := acceptanceFixture(t, mode, false, TaskBlocked)
			first, second := sup.State.Tasks[0], sup.State.Tasks[1]
			second.BlockedBy = []string{first.ID}
			sup.updateDependencies()
			if second.State != TaskBlocked || !second.DependencyBlocked {
				t.Fatalf("dependent Task: %+v", second)
			}
			sup.consumeControl()
			resumeFile(t, sup.State.Folder, Resume{Task: first.ID, Accept: true})
			sup.consumeControl()
			sup.schedule()
			if second.State != TaskRunning || second.DependencyBlocked || len(sup.running) != 1 {
				t.Fatalf("dependent Task did not start: %+v", second)
			}
			takeResult(t, sup)
			if second.State != TaskDone || sup.completionState() != StateFinished {
				t.Fatalf("dependent Task did not finish: %+v", second)
			}
		})
	}
}

func TestAcceptRejectsStatesAndMissingResults(t *testing.T) {
	for _, state := range []string{TaskRunning, TaskDone, TaskSkipped, TaskWaiting, TaskCancelled, TaskBlocked, TaskFailed} {
		t.Run(state, func(t *testing.T) {
			env := quietRunEnv(t)
			task := &TaskState{Number: 1, ID: "abcd", State: state}
			s := saveRun(t, env, task)
			if _, err := QueueResume(s.Folder, Resume{Task: task.ID, Accept: true}); err == nil || !strings.Contains(err.Error(), state) {
				t.Fatalf("accepted state %s: %v", state, err)
			}
		})
	}
	for _, missing := range []string{"dependency", "Report", "changed paths", "diff"} {
		t.Run(missing, func(t *testing.T) {
			sup, rec := acceptanceFixture(t, "tree", false, TaskBlocked)
			task := sup.State.Tasks[0]
			switch missing {
			case "dependency":
				task.DependencyBlocked = true
			case "Report":
				if err := os.Remove(filepath.Join(task.Folder, dispatch.FileReport)); err != nil {
					t.Fatal(err)
				}
			case "changed paths":
				rec.Verification.Changed = nil
			case "diff":
				if err := os.Remove(filepath.Join(sup.State.Project, "first.txt")); err != nil {
					t.Fatal(err)
				}
			}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			if err := sup.State.Save(); err != nil {
				t.Fatal(err)
			}
			if _, err := QueueResume(sup.State.Folder, Resume{Task: task.ID, Accept: true}); err == nil || !strings.Contains(err.Error(), TaskBlocked) {
				t.Fatalf("accepted without %s: %v", missing, err)
			}
			sup.applyResume(resumeFile(t, sup.State.Folder, Resume{Task: task.ID, Accept: true}))
			if task.State != TaskBlocked || task.Decision != "" {
				t.Fatalf("invalid acceptance applied: %+v", task)
			}
		})
	}
}

func TestAcceptCommitFailureKeepsTaskBlocked(t *testing.T) {
	sup, _ := acceptanceFixture(t, "tree", true, TaskBlocked)
	task := sup.State.Tasks[0]
	runWrite(t, filepath.Join(sup.State.Project, ".git", "hooks", "pre-commit"), "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(filepath.Join(sup.State.Project, ".git", "hooks", "pre-commit"), 0o755); err != nil {
		t.Fatal(err)
	}
	sup.consumeControl()
	resumeFile(t, sup.State.Folder, Resume{Task: task.ID, Accept: true})
	sup.consumeControl()
	if task.State != TaskBlocked || task.Decision == "accepted" || task.Error == "" {
		t.Fatalf("commit failure: %+v", task)
	}
}

func TestAcceptUsesExistingMergeConflictFlow(t *testing.T) {
	sup, rec := acceptanceFixture(t, "worktree", false, TaskFailed)
	task := sup.State.Tasks[0]
	pending := sup.pending[task]
	sup.State.Tasks = sup.State.Tasks[:1]
	runWrite(t, filepath.Join(task.Workdir, "base.txt"), "accepted version\n")
	rec.Verification.Changed = append(rec.Verification.Changed, "base.txt")
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	r, err := worktree.Open(sup.State.Project, sup.State.Run)
	if err != nil {
		t.Fatal(err)
	}
	other, err := r.Add("other")
	if err != nil {
		t.Fatal(err)
	}
	runWrite(t, filepath.Join(other.Dir, "base.txt"), "other version\n")
	if _, err := other.Commit("Change the shared file", []string{"base.txt"}); err != nil {
		t.Fatal(err)
	}
	if merged, err := r.Merge("other"); err != nil || len(merged.Conflicts) != 0 {
		t.Fatalf("merge other Task: %+v, %v", merged, err)
	}
	sup.applyResume(resumeFile(t, sup.State.Folder, Resume{Task: task.ID, Accept: true}))
	if task.State != TaskWaiting || !task.ConflictAttempt || len(task.Conflicts) != 1 || task.Conflicts[0] != "base.txt" || task.Decision != "accepted" {
		t.Fatalf("accepted merge did not use conflict flow: %+v", task)
	}
	sup.pending[task] = pending
	if !sup.start(task) {
		t.Fatalf("conflict attempt did not start: %+v", task)
	}
	takeResult(t, sup)
	if task.State != TaskBlocked || task.Error != "merge conflict in base.txt" {
		t.Fatalf("failed conflict attempt: %+v", task)
	}
}
