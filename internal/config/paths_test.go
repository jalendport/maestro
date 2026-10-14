package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestControlDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MAESTRO_HOME", home)
	if got, want := ControlDir("r-test", "abcd"), filepath.Join(home, "control", "r-test", "abcd"); got != want {
		t.Fatalf("ControlDir = %q, want %q", got, want)
	}
	for _, folder := range []string{"/project/.agents/maestro/runs/r-test/abcd", "/project/.agents/maestro/councils/r-test/abcd/"} {
		if got, want := ControlDirForFolder(folder), ControlDir("r-test", "abcd"); got != want {
			t.Fatalf("control folder = %q, want %q", got, want)
		}
	}
}

func TestFindProjectRoot(t *testing.T) {
	for _, tt := range []struct {
		name   string
		marker string
	}{
		{name: "git ancestor", marker: ".git"},
		{name: "project file ancestor", marker: ".agents/maestro.toml"},
		{name: "bare agents", marker: ".agents"},
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
				if tt.marker == ".agents/maestro.toml" {
					if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(root, tt.marker), nil, 0o644); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(filepath.Join(root, tt.marker), 0o755); err != nil {
					t.Fatal(err)
				}
				if tt.marker != ".agents" {
					want = root
				}
			}
			if got := FindProjectRoot(start); got != want {
				t.Fatalf("FindProjectRoot(%q) = %q, want %q", start, got, want)
			}
		})
	}
}

func TestFindProjectRootSkipsHomeMarkers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, marker := range []string{".git", ".agents/skills"} {
		if err := os.MkdirAll(filepath.Join(home, marker), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".agents", "maestro.toml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	start := filepath.Join(home, "work", "folder")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindProjectRoot(start); got != start {
		t.Fatalf("root = %q, want working directory %q", got, start)
	}
	if got := FindProjectRoot(home); got != "" {
		t.Fatalf("home must have no project root, got %q", got)
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

func TestControlDirForFolderRounds(t *testing.T) {
	t.Setenv("MAESTRO_HOME", "/m")
	if got := ControlDirForFolder("/p/.agents/maestro/councils/slug/round-2/grok"); got != "/m/control/slug/round-2/grok" {
		t.Fatalf("round folder mapped to %s", got)
	}
	if got := ControlDirForFolder("/p/.agents/maestro/runs/r-0001/abcd"); got != "/m/control/r-0001/abcd" {
		t.Fatalf("task folder mapped to %s", got)
	}
}
