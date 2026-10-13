package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectRoot(t *testing.T) {
	for _, tt := range []struct {
		name   string
		marker string
	}{
		{name: "git ancestor", marker: ".git"},
		{name: "agents ancestor", marker: ".agents"},
		{name: "no marker"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			start := filepath.Join(root, "nested", "child")
			if err := os.MkdirAll(start, 0o755); err != nil {
				t.Fatal(err)
			}
			want := start
			if tt.marker != "" {
				if err := os.Mkdir(filepath.Join(root, tt.marker), 0o755); err != nil {
					t.Fatal(err)
				}
				want = root
			}
			if got := FindProjectRoot(start); got != want {
				t.Fatalf("FindProjectRoot(%q) = %q, want %q", start, got, want)
			}
		})
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		path string
		want string
	}{
		{name: "home path", path: "~/x", want: filepath.Join(home, "x")},
		{name: "absolute path", path: "/abs/x", want: "/abs/x"},
		{name: "relative path", path: "rel/x", want: "rel/x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpandHome(tt.path); got != tt.want {
				t.Fatalf("ExpandHome(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestEnsureIgnoredDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "artifacts")
	if err := EnsureIgnoredDir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".gitignore")
	if got, err := os.ReadFile(path); err != nil || string(got) != "*\n" {
		t.Fatalf("ignore rules = %q, error = %v", got, err)
	}
	if err := os.WriteFile(path, []byte("custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIgnoredDir(dir); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "custom\n" {
		t.Fatalf("existing ignore rules = %q, error = %v", got, err)
	}
}
