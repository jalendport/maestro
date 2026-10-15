package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/proc"
)

func TestInitCreatesProjectFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, git := range []bool{true, false} {
		for _, machine := range []bool{false, true} {
			name := "plain"
			if git {
				name = "git"
			}
			if machine {
				name += " JSON"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				project := t.TempDir()
				cwd := project
				if git {
					if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
						t.Fatal(err)
					}
					cwd = filepath.Join(project, "child")
					if err := os.Mkdir(cwd, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				t.Chdir(cwd)
				path := config.ProjectFile(project)
				for _, state := range []string{"created", "unchanged"} {
					root := (&app{}).build()
					args := []string{"init"}
					if machine {
						args = []string{"--json", "init"}
					}
					root.SetArgs(args)
					output := captureStdout(t, root.Execute)
					if machine {
						decoder := json.NewDecoder(strings.NewReader(output))
						var result map[string]string
						if err := decoder.Decode(&result); err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(result, map[string]string{"path": path, "state": state}) {
							t.Fatalf("result = %v", result)
						}
						if err := decoder.Decode(&result); err != io.EOF {
							t.Fatalf("extra output = %q, error = %v", output, err)
						}
					} else if output != state+" "+path+"\n" {
						t.Fatalf("output = %q", output)
					}
					content, err := os.ReadFile(path)
					if err != nil || string(content) != projectTemplate {
						t.Fatalf("content = %q, error = %v", content, err)
					}
				}
				loaded, err := config.LoadProject(project)
				if err != nil || !reflect.DeepEqual(loaded, &config.Project{}) {
					t.Fatalf("Project = %+v, error = %v", loaded, err)
				}
			})
		}
	}
}

func TestInitPreservesExistingProjectFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := config.ProjectFile(project)
	content := []byte("# custom Project file\nchecks = [\"go test ./...\"]\n")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	root := (&app{}).build()
	root.SetArgs([]string{"init"})
	if output := captureStdout(t, root.Execute); output != "unchanged "+path+"\n" {
		t.Fatalf("output = %q", output)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(content) {
		t.Fatalf("content = %q, error = %v", got, err)
	}
}

func TestInitRefusesNonRegularProjectFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, kind := range []string{"directory", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			project := t.TempDir()
			path := config.ProjectFile(project)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(filepath.Join(project, "missing"), path); err != nil {
				t.Fatal(err)
			}
			t.Chdir(project)
			root := (&app{}).build()
			root.SetArgs([]string{"init"})
			if err := root.Execute(); err == nil || err.Error() != path+" exists and is not a regular file" {
				t.Fatalf("error = %v", err)
			}
			previous := os.Args
			os.Args = []string{"maestro", "init"}
			t.Cleanup(func() { os.Args = previous })
			var code int
			output := captureStdout(t, func() error { code = Execute("test"); return nil })
			if code != proc.CodeFailed {
				t.Fatalf("exit code = %d", code)
			}
			if output != "" {
				t.Fatalf("output = %q", output)
			}
		})
	}
}

func TestInitRefusesHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(home)
	root := (&app{}).build()
	root.SetArgs([]string{"init"})
	if err := root.Execute(); err == nil || err.Error() != "maestro init needs a project directory" {
		t.Fatalf("error = %v", err)
	}
	previous := os.Args
	os.Args = []string{"maestro", "init"}
	t.Cleanup(func() { os.Args = previous })
	var code int
	captureStdout(t, func() error { code = Execute("test"); return nil })
	if code != proc.CodeFailed {
		t.Fatalf("exit code = %d", code)
	}
}

func TestInitTemplateIncludesEveryProjectKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	lines := strings.Split(strings.TrimSpace(projectTemplate), "\n")
	if lines[0] != "# Maestro project file. Its presence puts this project in hook scope." {
		t.Fatalf("header = %q", lines[0])
	}
	var source strings.Builder
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, "# ") || !strings.Contains(line, "   # ") {
			t.Fatalf("template line = %q", line)
		}
		source.WriteString(strings.TrimPrefix(line, "# ") + "\n")
	}
	var project config.Project
	metadata, err := toml.Decode(source.String(), &project)
	if err != nil || !reflect.DeepEqual(project, config.Project{
		Forbid: []string{}, Checks: []string{}, Allow: []string{}, Redact: []string{}, Dirs: []string{},
	}) {
		t.Fatalf("Project = %+v, error = %v", project, err)
	}
	shape := reflect.TypeOf(project)
	keys := 0
	for i := 0; i < shape.NumField(); i++ {
		key := shape.Field(i).Tag.Get("toml")
		if key == "-" {
			continue
		}
		keys++
		if !metadata.IsDefined(key) {
			t.Fatalf("template is missing %q", key)
		}
	}
	if len(lines)-1 != keys || len(metadata.Undecoded()) != 0 {
		t.Fatalf("template lines = %d, keys = %d, unknown = %v", len(lines)-1, keys, metadata.Undecoded())
	}
}

func TestInitProjectFileAppearsInDoctor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("GROK_HOME", t.TempDir())
	project := t.TempDir()
	t.Chdir(project)
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("PATH", "")
	t.Setenv("SOLO_PROCESS_ID", "")
	root := (&app{}).build()
	root.SetArgs([]string{"init"})
	captureStdout(t, root.Execute)
	root = (&app{}).build()
	root.SetArgs([]string{"doctor"})
	output := captureStdout(t, root.Execute)
	if strings.Contains(output, "project file: none") || !strings.Contains(output, "project "+config.ProjectFile(project)) {
		t.Fatalf("doctor output = %q", output)
	}
}

func TestInitAppearsBeforeAdapterInHelp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := (&app{}).build()
	root.SetArgs([]string{"--help"})
	output := captureStdout(t, root.Execute)
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Write the project file .agents/maestro.toml, which puts the project in hook scope") {
			if !strings.Contains(line, "init") || i+1 >= len(lines) || !strings.Contains(lines[i+1], "adapter [harness]") {
				t.Fatalf("init help is not before adapter: %q", output)
			}
			return
		}
	}
	t.Fatalf("init help is missing: %q", output)
}
