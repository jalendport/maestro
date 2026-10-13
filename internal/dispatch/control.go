package dispatch

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const fileCancel = "cancel"

// RequestCancel asks the detached supervisor to stop its Task.
func RequestCancel(folder string) error {
	return os.WriteFile(filepath.Join(folder, fileCancel), nil, 0o644)
}

// WatchCancel connects a detached supervisor to the Lane's Cancel channel.
func WatchCancel(ctx context.Context, folder string) <-chan struct{} {
	cancelled := make(chan struct{})
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := os.Remove(filepath.Join(folder, fileCancel)); err == nil {
				close(cancelled)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return cancelled
}

// HasRunningRecord protects live Dispatches and Councils even without an index.
// An unreadable record cannot prove that the folder is safe to remove.
func HasRunningRecord(folder string) bool {
	live := false
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (entry.Name() != FileRecord && entry.Name() != "council.json") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal(b, &record); err != nil {
			return err
		}
		if record.State == "running" {
			live = true
			return fs.SkipAll
		}
		return nil
	})
	return live || err != nil
}
