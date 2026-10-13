package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
)

func TestNextRunID(t *testing.T) {
	for _, tt := range []struct {
		name string
		runs []string
		want string
	}{
		{name: "empty project", want: "r-0001"},
		{name: "existing runs", runs: []string{"r-0001", "r-0007"}, want: "r-0008"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			for _, run := range tt.runs {
				if err := os.MkdirAll(filepath.Join(config.RunsDir(root), run), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := NextRunID(root); err != nil || got != tt.want {
				t.Fatalf("NextRunID() = %q, %v, want %q", got, err, tt.want)
			}
			if info, err := os.Stat(filepath.Join(config.RunsDir(root), tt.want)); err != nil || !info.IsDir() {
				t.Fatalf("reserved folder: info=%v err=%v", info, err)
			}
		})
	}
}

func TestNextRunIDConcurrent(t *testing.T) {
	root := t.TempDir()
	const count = 32
	start := make(chan struct{})
	ids := make(chan string, count)
	for i := 0; i < count; i++ {
		go func() {
			<-start
			id, err := NextRunID(root)
			if err != nil {
				t.Error(err)
			}
			ids <- id
		}()
	}
	close(start)
	seen := map[string]bool{}
	for i := 0; i < count; i++ {
		id := <-ids
		if seen[id] {
			t.Fatalf("Run id %q was reserved twice", id)
		}
		seen[id] = true
		if info, err := os.Stat(filepath.Join(config.RunsDir(root), id)); err != nil || !info.IsDir() {
			t.Fatalf("reserved folder %s: info=%v err=%v", id, info, err)
		}
	}
}

func TestNextRunIDAllocationError(t *testing.T) {
	root := t.TempDir()
	runsDir := config.RunsDir(root)
	if err := os.MkdirAll(filepath.Dir(runsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runsDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := NextRunID(root)
	if id != "" || err == nil || !strings.Contains(err.Error(), "reserve Run id:") {
		t.Fatalf("NextRunID() = %q, %v, want Run id allocation error", id, err)
	}
}

func TestNewTaskID(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(config.RunsDir(root), "r-0001")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{4}$`)
	for i := 0; i < 128; i++ {
		id := NewTaskID(runDir)
		if !pattern.MatchString(id) {
			t.Fatalf("NewTaskID() = %q, want four lowercase hex characters", id)
		}
		path := filepath.Join(runDir, id)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("NewTaskID() = %q, want an unused folder; stat error: %v", id, err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewTaskIDAvoidsExistingFolders(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(config.RunsDir(root), "r-0001")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const freeID = "abcd"
	for i := 0; i < 1<<16; i++ {
		id := fmt.Sprintf("%04x", i)
		if id == freeID {
			continue
		}
		if err := os.Mkdir(filepath.Join(runDir, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := NewTaskID(runDir); got != freeID {
		t.Fatalf("NewTaskID() = %q, want the only unused folder %q", got, freeID)
	}
}

func TestFindTask(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(config.RunsDir(root), "r-0007")
	taskDir := filepath.Join(runDir, "4f73")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&Task{Folder: taskDir, Task: "4f73"}).Save(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		id   string
		want string
		ok   bool
	}{
		{name: "task id", id: "4f73", want: taskDir, ok: true},
		{name: "run and task id", id: "r-0007/4f73", want: taskDir, ok: true},
		{name: "run id", id: "r-0007", want: taskDir, ok: true},
		{name: "unknown id", id: "ffff"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FindTask(root, tt.id)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("FindTask(%q) = (%q, %t), want (%q, %t)", tt.id, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestFindTaskUsesNewestNumericRun(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"r-0001", "r-9999", "r-10000"} {
		folder := filepath.Join(config.RunsDir(root), id, "abcd")
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := filepath.Join(config.RunsDir(root), "r-10000", "abcd")
	if got, ok := FindTask(root, "abcd"); !ok || got != want {
		t.Fatalf("FindTask = %q, %v; want %q", got, ok, want)
	}
}

func TestFindTaskRejectsAmbiguousRun(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"abcd", "efab"} {
		folder := filepath.Join(config.RunsDir(root), "r-0001", id)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := (&Task{Folder: folder}).Save(); err != nil {
			t.Fatal(err)
		}
	}
	if folder, ok := FindTask(root, "r-0001"); ok {
		t.Fatalf("ambiguous Run resolved to %s", folder)
	}
}
