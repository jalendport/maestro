package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/worktree"
)

func cleanStandaloneWorktrees(folder string) ([]string, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	var kept []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		task, err := dispatch.LoadTask(filepath.Join(folder, entry.Name()))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return kept, err
		}
		if task.Standalone == nil || task.LedgerByRun || task.Council || task.Access != brief.AccessEdit || task.Workdir == "" || task.Workdir == task.Project {
			continue
		}
		id, err := dispatch.StandaloneWorktreeID(task)
		if err != nil {
			return kept, err
		}
		branches, err := worktree.Remove(task.Project, id)
		kept = append(kept, branches...)
		if err != nil {
			return kept, err
		}
	}
	return kept, nil
}

func hasCurrentRecord(folder string, cutoff time.Time) (bool, error) {
	keep := false
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (entry.Name() != dispatch.FileRecord && entry.Name() != "run.json" && entry.Name() != "council.json") {
			return nil
		}
		exists, expired, err := stateFileExpired(path, cutoff)
		if err != nil {
			return err
		}
		if exists && !expired {
			keep = true
			return fs.SkipAll
		}
		return nil
	})
	return keep, err
}

func latestCanaryFolders() (map[string]bool, error) {
	folders := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(config.Home(), "canaries"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-len(".json")]
		result, err := dispatch.LoadCanary(name)
		if err != nil {
			return nil, err
		}
		if result != nil && result.ScratchFolder != "" {
			folders[filepath.Clean(result.ScratchFolder)] = true
		}
	}
	return folders, nil
}

func cleanCacheFolders(cutoff time.Time, canaries map[string]bool) (int, error) {
	root := filepath.Join(config.Home(), "cache")
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return removed, err
		}
		if canaries[path] || canaries[resolved] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(path); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

func cleanControlFolders(cutoff time.Time, protected map[string]bool) (int, error) {
	root := filepath.Join(config.Home(), "control")
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if config.IsProjectKey(entry.Name()) && entry.IsDir() {
			n, err := cleanProjectControl(path, cutoff, protected)
			if err != nil {
				return removed, err
			}
			removed += n
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(path); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

func cleanProjectControl(path string, cutoff time.Time, protected map[string]bool) (int, error) {
	if protected[path] {
		return 0, nil
	}
	b, err := os.ReadFile(filepath.Join(path, "lane.json"))
	if err == nil {
		var lane hook.Lane
		if err := json.Unmarshal(b, &lane); err != nil {
			return 0, err
		}
		if lane.RunFolder != "" {
			expired, err := controlRecordExpired(lane.RunFolder, cutoff)
			if err != nil || !expired {
				return 0, err
			}
		}
		return 1, os.RemoveAll(path)
	}
	if !os.IsNotExist(err) {
		return 0, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			n, err := cleanProjectControl(filepath.Join(path, entry.Name()), cutoff, protected)
			if err != nil {
				return removed, err
			}
			removed += n
		}
	}
	entries, err = os.ReadDir(path)
	if err != nil {
		return removed, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return removed, nil
		}
	}
	// No policy or child folder remains: this control folder is an orphan.
	return removed + 1, os.RemoveAll(path)
}

func controlRecordExpired(folder string, cutoff time.Time) (bool, error) {
	for parent := filepath.Dir(folder); parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if filepath.Base(parent) == "runs" || filepath.Base(parent) == "councils" {
			break
		}
		for _, name := range []string{"run.json", "council.json"} {
			exists, expired, err := stateFileExpired(filepath.Join(parent, name), cutoff)
			if err != nil || (exists && !expired) {
				return false, err
			}
		}
	}
	exists, expired, err := stateFileExpired(filepath.Join(folder, dispatch.FileRecord), cutoff)
	return !exists || expired, err
}

func stateFileExpired(path string, cutoff time.Time) (bool, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	var record struct {
		State   string    `json:"state"`
		Mode    string    `json:"mode"`
		EndedAt time.Time `json:"ended_at"`
	}
	if err := json.Unmarshal(b, &record); err != nil {
		return true, false, err
	}
	if record.State != "ended" && record.State != "finished" && record.State != "cancelled" && !(record.State == "paused" && record.Mode == "worktree" && !record.EndedAt.IsZero()) {
		return true, false, nil
	}
	if record.EndedAt.IsZero() {
		info, err := os.Stat(path)
		if err != nil {
			return true, false, err
		}
		record.EndedAt = info.ModTime()
	}
	return true, record.EndedAt.Before(cutoff), nil
}
