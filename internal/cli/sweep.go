package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/run"
	"github.com/jalendport/maestro/internal/ui"
	"github.com/jalendport/maestro/internal/worktree"
)

type keptWorktree struct {
	ID          string `json:"id"`
	Reason      string `json:"reason"`
	NextCommand string `json:"next_command"`
}

// sweepWorktrees checks only this project's saved identities. Errors leave the
// verb usable and do not stop checks of the other identities.
func sweepWorktrees(root string) []keptWorktree {
	kept := []keptWorktree{}
	removed := 0
	check := func(id, command, pending string) {
		branch, exists, err := sweepIdentity(root, id)
		if err != nil {
			ui.Warnf("Worktree sweep %s: %v", id, err)
			kept = append(kept, keptWorktree{ID: id, Reason: err.Error(), NextCommand: command})
			return
		}
		if !exists {
			return
		}
		landed, reason, err := worktree.Landed(root, id, branch)
		if err == nil && landed && pending == "" {
			err = worktree.RemoveLanded(root, id, branch)
			if err == nil {
				removed++
				return
			}
		}
		if err != nil {
			ui.Warnf("Worktree sweep %s: %v", id, err)
			reason = err.Error()
		} else if reason == "" {
			reason = pending
		}
		kept = append(kept, keptWorktree{ID: id, Reason: reason, NextCommand: command})
	}
	folders, err := dispatch.RunFolders(root)
	if err != nil && !os.IsNotExist(err) {
		ui.Warnf("Worktree sweep: %v", err)
	}
	for _, entry := range folders {
		folder := filepath.Join(config.RunsDir(root), entry.Name())
		s, err := run.Load(folder)
		if err == nil {
			if s.Mode != "worktree" && s.RunBranch == "" {
				continue
			}
			if run.SupervisorAlive(s) || s.State == run.StateLive || s.State == run.StateHeld || s.State == run.StatePaused {
				continue
			}
			command, pending := "maestro clean --older-than 30d", ""
			if s.FinishPending {
				command = "maestro resume " + s.Run + " --finish"
				pending = "landing waits for Finish"
			}
			for _, task := range s.Tasks {
				if run.NeedsAnswer(s, task) {
					pending = "Task waits for an Orchestrator answer"
				}
			}
			check(s.Run, command, pending)
			continue
		}
		if !os.IsNotExist(err) {
			ui.Warnf("Worktree sweep: %v", err)
			continue
		}
		tasks, err := os.ReadDir(folder)
		if err != nil {
			ui.Warnf("Worktree sweep: %v", err)
			continue
		}
		for _, entry := range tasks {
			if !entry.IsDir() {
				continue
			}
			folder := filepath.Join(folder, entry.Name())
			t, err := dispatch.LoadTask(folder)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				ui.Warnf("Worktree sweep: %v", err)
				continue
			}
			if t.Standalone == nil || t.LedgerByRun || t.Council || t.Access != "edit" || t.Workdir == "" || t.Workdir == root {
				continue
			}
			rec, err := dispatch.LoadRecord(folder)
			if err != nil {
				ui.Warnf("Worktree sweep: %v", err)
				continue
			}
			if dispatch.ProcessAlive(rec) {
				continue
			}
			id, err := dispatch.StandaloneWorktreeID(t)
			if err != nil {
				ui.Warnf("Worktree sweep: %v", err)
				continue
			}
			command, pending := "maestro clean --older-than 30d", ""
			if rec.FinishPending {
				command, pending = "maestro dispatch --finish "+t.Task, "landing waits for Finish"
			}
			check(id, command, pending)
		}
	}
	if removed > 0 {
		fmt.Fprintf(os.Stderr, "cleaned %d landed Worktrees\n", removed)
	}
	return kept
}

// Read the identity without Open: Open would create a new branch or folder.
func sweepIdentity(root, id string) (branch string, exists bool, err error) {
	if id == "" || strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return "", false, fmt.Errorf("invalid Worktree identity %q", id)
	}
	control, err := config.ControlDirForFolder(filepath.Join(config.RunsDir(root), id))
	if err != nil {
		return "", false, err
	}
	dir := filepath.Join(config.Home(), "worktrees", filepath.Base(filepath.Dir(control)), id)
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err == nil {
		var saved worktree.Run
		if err := json.Unmarshal(data, &saved); err != nil {
			return "", true, err
		}
		branch, exists = saved.UserBranch, true
	} else if !os.IsNotExist(err) {
		return "", false, err
	} else {
		err = nil
		_, statErr := os.Stat(dir)
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", false, statErr
		}
		exists = statErr == nil
		out, refErr := exec.Command("git", "-C", root, "for-each-ref", "--format=%(refname)", "refs/heads/maestro/"+id+"/").Output()
		if refErr != nil {
			return "", exists, refErr
		}
		exists = exists || len(out) > 0
	}
	if exists && branch == "" {
		out, branchErr := exec.Command("git", "-C", root, "symbolic-ref", "--quiet", "HEAD").Output()
		branch, err = strings.TrimSpace(string(out)), branchErr
		if branch == "" && err == nil {
			err = errors.New("no checked-out user branch")
		}
	}
	return branch, exists, err
}
