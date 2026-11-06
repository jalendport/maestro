package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

const fileCancel = "cancel"

// RequestCancel asks the detached supervisor to stop its Task.
func RequestCancel(folder string) error {
	return os.WriteFile(filepath.Join(folder, fileCancel), nil, 0o644)
}

// MonitorCancel connects a detached supervisor to the Lane's Cancel channel.
func MonitorCancel(ctx context.Context, folder string) <-chan struct{} {
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
