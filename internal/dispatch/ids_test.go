package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
			if got := NextRunID(root); got != tt.want {
				t.Fatalf("NextRunID() = %q, want %q", got, tt.want)
			}
		})
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
	for _, tt := range []struct {
		name string
		id   string
		want string
		ok   bool
	}{
		{name: "task id", id: "4f73", want: taskDir, ok: true},
		{name: "run and task id", id: "r-0007/4f73", want: taskDir, ok: true},
		{name: "run id", id: "r-0007", want: runDir, ok: true},
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
