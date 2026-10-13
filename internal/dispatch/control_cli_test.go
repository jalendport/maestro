package dispatch_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/router"
)

func standaloneTask(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "")
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&discovery.Cache{RefreshedAt: time.Now()}).Save(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&dispatch.Task{Folder: folder, Project: root, Run: "r-0001", Task: "abcd"}).Save(); err != nil {
		t.Fatal(err)
	}
	return root, folder
}

func TestCancelStandaloneReachesSuperviseLoop(t *testing.T) {
	_, folder := standaloneTask(t)
	if err := (&dispatch.Record{Path: folder, Task: "abcd", State: "running"}).Save(); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	stopped := make(chan struct{})
	go func() {
		select {
		case <-dispatch.WatchCancel(ctx, folder):
			close(stopped)
		case <-ctx.Done():
		}
	}()
	out, code := cliOutput(t, "cancel", "abcd")
	if code != 0 {
		t.Fatalf("cancel: %d %s", code, out)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("supervise loop did not consume cancel file")
	}
	if _, err := os.Stat(filepath.Join(folder, "cancel")); !os.IsNotExist(err) {
		t.Fatalf("cancel file remains: %v", err)
	}
}

func TestWaitStandaloneRunID(t *testing.T) {
	_, folder := standaloneTask(t)
	rec := &dispatch.Record{Path: folder, Task: "abcd", State: "ended", Route: &router.Route{}, Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeAnswered}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	out, code := cliOutput(t, "wait", "r-0001", "--json")
	if code != 0 || !strings.Contains(out, `"task": "abcd"`) {
		t.Fatalf("wait: %d %s", code, out)
	}
}

func TestCleanSkipsRunningRecordsWithoutIndex(t *testing.T) {
	root, folder := standaloneTask(t)
	if err := (&dispatch.Record{Path: folder, State: "running"}).Save(); err != nil {
		t.Fatal(err)
	}
	councilFolder := filepath.Join(config.CouncilsDir(root), "live")
	ended := filepath.Join(config.RunsDir(root), "r-0002")
	for _, path := range []string{councilFolder, ended} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(councilFolder, "council.json"), []byte(`{"state":"running"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := cliOutput(t, "clean", "--older-than", "0h")
	if code != 0 {
		t.Fatalf("clean: %d %s", code, out)
	}
	for _, path := range []string{folder, councilFolder} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("running folder removed: %s: %v", path, err)
		}
	}
	if _, err := os.Stat(ended); !os.IsNotExist(err) {
		t.Fatalf("ended folder retained: %v", err)
	}
}
