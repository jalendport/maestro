package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAncestor(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bins      []string
		processes map[int]Process
		want      int
	}{
		{"nearest", []string{"/apps/first", "/apps/second"}, map[int]Process{9: {Parent: 8, Executable: "/bin/sh"}, 8: {Parent: 7, Executable: "/apps/first"}, 7: {Parent: 1, Executable: "/apps/second"}}, 8},
		{"absent", []string{"/apps/harness"}, map[int]Process{9: {Parent: 1, Executable: "/bin/sh"}}, 0},
		{"lookup error", []string{"/apps/harness"}, nil, 0},
		{"cycle", []string{"/apps/harness"}, map[int]Process{9: {Parent: 8}, 8: {Parent: 9}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Ancestor(9, tc.bins, func(pid int) (Process, error) {
				p, ok := tc.processes[pid]
				if !ok {
					return Process{}, fmt.Errorf("gone")
				}
				return p, nil
			})
			if got != tc.want {
				t.Fatalf("ancestor = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAncestorResolvesConfiguredSymlink(t *testing.T) {
	dir := t.TempDir()
	bin, link := filepath.Join(dir, "real"), filepath.Join(dir, "configured")
	if err := os.WriteFile(bin, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	got := Ancestor(9, []string{link}, func(int) (Process, error) { return Process{Parent: 1, Executable: bin}, nil })
	if got != 9 {
		t.Fatalf("ancestor = %d", got)
	}
}

func TestSnapshotProcessIdentity(t *testing.T) {
	p, err := SnapshotProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if p.Parent != os.Getppid() || p.Executable == "" || p.Started == "" {
		t.Fatalf("process identity = %+v", p)
	}
	again, err := SnapshotProcess(os.Getpid())
	if err != nil || again.Started != p.Started {
		t.Fatalf("process start time changed: %+v, %v", again, err)
	}
	if _, err := SnapshotProcess(1 << 30); err == nil {
		t.Fatal("missing process returned an identity")
	}
}
