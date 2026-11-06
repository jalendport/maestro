package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/jalendport/maestro/internal/config"
)

func TestInitDetectsProjectStacks(t *testing.T) {
	goCache := "~/.cache/go-build"
	if runtime.GOOS == "darwin" {
		goCache = "~/Library/Caches/go-build"
	}
	goAllow := []string{"Bash(go test *)", "Bash(go vet *)", "Bash(go build *)", "Bash(gofmt *)"}
	dockerAllow := []string{"Bash(docker compose *)", "Bash(docker build *)"}
	for _, tc := range []struct {
		name          string
		markers       []string
		dirs, allow   []string
		checks        []string
		bin, args     string
		queriedCaches int
	}{
		{"Go", []string{"go.mod"}, []string{goCache, "~/go/pkg/mod"}, goAllow, []string{"go test ./..."}, "go", "env GOCACHE GOMODCACHE", 2},
		{"npm", []string{"package.json"}, []string{"~/.npm"}, []string{"Bash(npm test)", "Bash(npm test *)", "Bash(npm run *)"}, []string{"npm test"}, "npm", "config get cache", 1},
		{"npm lock", []string{"package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock"}, []string{"~/.npm"}, []string{"Bash(npm test)", "Bash(npm test *)", "Bash(npm run *)"}, []string{"npm test"}, "npm", "config get cache", 1},
		{"pnpm", []string{"package.json", "pnpm-lock.yaml"}, []string{"~/.cache/pnpm"}, []string{"Bash(pnpm test)", "Bash(pnpm test *)", "Bash(pnpm run *)"}, []string{"pnpm test"}, "pnpm", "store path", 1},
		{"yarn", []string{"package.json", "yarn.lock"}, []string{"~/.yarn", "~/.cache/yarn"}, []string{"Bash(yarn test)", "Bash(yarn test *)", "Bash(yarn run *)"}, []string{"yarn test"}, "yarn", "cache dir", 1},
		{"PHP", []string{"composer.json"}, []string{"~/.composer/cache", "~/.cache/composer"}, []string{"Bash(composer test)", "Bash(composer test *)", "Bash(vendor/bin/phpunit *)", "Bash(vendor/bin/pint *)", "Bash(vendor/bin/phpstan *)"}, []string{"composer test"}, "composer", "config --global cache-dir", 1},
		{"Python project", []string{"pyproject.toml"}, []string{"~/.cache/pip"}, []string{"Bash(pytest)", "Bash(pytest *)", "Bash(ruff *)"}, []string{"pytest"}, "pip", "cache dir", 1},
		{"Python requirements", []string{"requirements.txt"}, []string{"~/.cache/pip"}, []string{"Bash(pytest)", "Bash(pytest *)", "Bash(ruff *)"}, []string{"pytest"}, "pip", "cache dir", 1},
		{"Rust", []string{"Cargo.toml"}, []string{"~/.cargo"}, []string{"Bash(cargo test *)", "Bash(cargo build *)", "Bash(cargo clippy *)", "Bash(cargo fmt *)"}, []string{"cargo test"}, "", "", 0},
		{"Dockerfile", []string{"Dockerfile"}, []string{"~/.docker"}, dockerAllow, []string{"docker compose config --quiet"}, "", "", 0},
		{"compose yaml", []string{"compose.yaml"}, []string{"~/.docker"}, dockerAllow, []string{"docker compose config --quiet"}, "", "", 0},
		{"compose yml", []string{"compose.yml"}, []string{"~/.docker"}, dockerAllow, []string{"docker compose config --quiet"}, "", "", 0},
		{"docker compose yml", []string{"docker-compose.yml"}, []string{"~/.docker"}, dockerAllow, []string{"docker compose config --quiet"}, "", "", 0},
		{"Go and Docker", []string{"go.mod", "Dockerfile"}, []string{goCache, "~/go/pkg/mod", "~/.docker"}, append(slices.Clone(goAllow), dockerAllow...), []string{"go test ./...", "docker compose config --quiet"}, "go", "env GOCACHE GOMODCACHE", 2},
	} {
		for _, stub := range []bool{false, true} {
			name := tc.name + "/fallback"
			if stub {
				name = tc.name + "/toolchain"
			}
			t.Run(name, func(t *testing.T) {
				home, project := t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("PATH", "")
				t.Chdir(project)
				for _, marker := range tc.markers {
					initTestFile(t, filepath.Join(project, marker), "")
				}
				wantDirs := slices.Clone(tc.dirs)
				if stub {
					bin := t.TempDir()
					t.Setenv("PATH", bin)
					if tc.bin != "" {
						paths := []string{filepath.Join(home, "cache with spaces #[]")}
						if tc.queriedCaches == 2 {
							paths = append(paths, filepath.Join(home, "module cache"))
						}
						script := "#!/bin/sh\n[ \"$*\" = '" + tc.args + "' ] || exit 1\nprintf '%s\\n'"
						for _, path := range paths {
							script += " '" + path + "'"
						}
						initTestFile(t, filepath.Join(bin, tc.bin), script+"\n")
						if err := os.Chmod(filepath.Join(bin, tc.bin), 0o755); err != nil {
							t.Fatal(err)
						}
						if tc.bin == "composer" {
							wantDirs = paths
						} else if tc.bin == "yarn" {
							wantDirs = append([]string{"~/.yarn"}, paths...)
						} else {
							wantDirs = append(paths, tc.dirs[tc.queriedCaches:]...)
						}
					}
				}
				docker := slices.Contains(tc.dirs, "~/.docker")
				if stub && docker {
					if err := os.MkdirAll(filepath.Join(home, ".orbstack", "run"), 0o755); err != nil {
						t.Fatal(err)
					}
					wantDirs = append(wantDirs, "~/.orbstack/run")
				}
				command := (&app{}).build()
				command.SetArgs([]string{"init"})
				captureStdout(t, command.Execute)
				loaded, err := config.LoadProject(project)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(loaded.Dirs, wantDirs) || !reflect.DeepEqual(loaded.Allow, tc.allow) || len(loaded.Checks) != 0 {
					t.Fatalf("Project = %+v, want dirs %v and allow %v", loaded, wantDirs, tc.allow)
				}
				content, err := os.ReadFile(config.ProjectFile(project))
				if err != nil || !strings.Contains(string(content), "# checks = "+projectArray(tc.checks)) {
					t.Fatalf("content = %s, error = %v", content, err)
				}
				if docker && (!strings.Contains(string(content), "# allow = [\"Bash(docker run *)\"]") || slices.Contains(loaded.Allow, "Bash(docker run *)")) {
					t.Fatalf("Docker run suggestion = %s", content)
				}
			})
		}
	}
}

func TestInitMergePreservesTOML(t *testing.T) {
	for _, source := range []string{
		"custom.allow = ['keep this value']\n",
		"'allow' = [\n 'Bash(npm test)', # keep this\n]\nchecks = []\n",
		"allow = [\"\"\"Bash(npm test)\"\"\" # keep this\n]\n# 'checks' = []\n",
		"allow = [] # keep this\n# checks = []\n[custom]\nvalue = 'leave this alone'\n",
		"# keep this\n[custom]\nvalue = '''\nallow = []\n# checks = []\n'''\n",
	} {
		t.Run(source, func(t *testing.T) {
			defaults := projectDefaults{allow: []string{"Bash(npm test)", "Bash(npm run *)"}, checks: []string{"npm test"}}
			merged, err := mergeProjectDefaults(source, defaults)
			if err != nil {
				t.Fatal(err)
			}
			var project config.Project
			if _, err := toml.Decode(merged, &project); err != nil || !reflect.DeepEqual(project.Allow, defaults.allow) {
				t.Fatalf("merged = %s, Project = %+v, error = %v", merged, project, err)
			}
			for _, text := range []string{"# keep this", "value = 'leave this alone'", "value = '''\nallow = []\n# checks = []\n'''"} {
				if strings.Contains(source, text) && !strings.Contains(merged, text) {
					t.Fatalf("lost %q in %s", text, merged)
				}
			}
			again, err := mergeProjectDefaults(merged, defaults)
			if err != nil || again != merged {
				t.Fatalf("second merge = %s, error = %v", again, err)
			}
		})
	}
}

func TestInitIgnoresLockfilesWithoutPackageJSON(t *testing.T) {
	t.Setenv("PATH", "")
	project := t.TempDir()
	for _, marker := range []string{"package-lock.json", "pnpm-lock.yaml", "yarn.lock"} {
		initTestFile(t, filepath.Join(project, marker), "")
	}
	if defaults := detectProjectStack(project); !reflect.DeepEqual(defaults, projectDefaults{}) {
		t.Fatalf("defaults = %+v", defaults)
	}
}

func initTestFile(t *testing.T, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInitWorktreeLinks(t *testing.T) {
	for _, stack := range []struct {
		name, dependency string
		links            []string
	}{
		{"Craft", "craftcms/cms", []string{"storage", "web/cpresources", "config/license.key"}},
		{"Laravel", "laravel/framework", []string{"storage", "bootstrap/cache"}},
	} {
		for _, present := range []bool{false, true} {
			t.Run(stack.name+map[bool]string{false: "/missing", true: "/present"}[present], func(t *testing.T) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("PATH", "")
				root := t.TempDir()
				t.Chdir(root)
				initTestFile(t, filepath.Join(root, "composer.json"), `{"require":{"`+stack.dependency+`":"*"}}`)
				if present {
					for _, name := range stack.links {
						initTestFile(t, filepath.Join(root, name), "local data")
					}
				}
				command := (&app{}).build()
				command.SetArgs([]string{"init"})
				captureStdout(t, command.Execute)
				data, err := os.ReadFile(config.ProjectFile(root))
				prefix := "# "
				if present {
					prefix = ""
				}
				if err != nil || !strings.Contains(string(data), "\n"+prefix+"worktree_links = "+projectArray(stack.links)+"\n") {
					t.Fatalf("Project file: %s, %v", data, err)
				}
				loaded, err := config.LoadProject(root)
				if err != nil || len(loaded.Unknown) > 0 || (len(loaded.WorktreeLinks) > 0) != present {
					t.Fatalf("parsed links: %+v, %v", loaded, err)
				}
				if got := proposeWorktreeLinks(root, string(data), stack.links); got != string(data) {
					t.Fatal("second proposal changed the file")
				}
			})
		}
	}
}

func TestInitPreviewsAndMergesExistingProject(t *testing.T) {
	for _, machine := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "JSON"}[machine], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("PATH", "")
			project := t.TempDir()
			t.Chdir(project)
			initTestFile(t, filepath.Join(project, "package.json"), "{}")
			path := config.ProjectFile(project)
			source := "# Keep this choice.\ndirs = ['~/.npm']\nallow = [\"Bash(npm test)\"] # Keep this comment.\n# allow = [\"Bash(npm test *)\"]\n# checks = [\"npm test\"]\nrouter = 'rules'\n"
			initTestFile(t, path, source)
			run := func(args ...string) string {
				command := (&app{}).build()
				if machine {
					args = append([]string{"--json"}, args...)
				}
				command.SetArgs(args)
				return captureStdout(t, command.Execute)
			}
			preview := run("init")
			if machine {
				var result map[string]string
				if err := json.Unmarshal([]byte(preview), &result); err != nil || result["state"] != "unchanged" || !strings.Contains(result["proposed"], "Bash(npm run *)") {
					t.Fatalf("preview = %s, error = %v", preview, err)
				}
			} else if !strings.Contains(preview, "--write") || !strings.Contains(preview, "Bash(npm run *)") {
				t.Fatalf("preview = %s", preview)
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || string(unchanged) != source {
				t.Fatalf("preview changed source: %s, error = %v", unchanged, err)
			}
			if output := run("init", "--write"); !strings.Contains(output, "updated") {
				t.Fatalf("output = %s", output)
			}
			merged, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := config.LoadProject(project)
			if err != nil || !reflect.DeepEqual(loaded.Allow, []string{"Bash(npm test)", "Bash(npm run *)"}) || loaded.Router != "rules" || !strings.Contains(string(merged), "# Keep this comment.") || !strings.Contains(string(merged), "# allow = [\"Bash(npm test *)\"]") || strings.Count(string(merged), "# checks =") != 1 {
				t.Fatalf("merged = %s, Project = %+v, error = %v", merged, loaded, err)
			}
			if output := run("init", "--write"); !strings.Contains(output, "unchanged") {
				t.Fatalf("second merge = %s", output)
			}
			again, err := os.ReadFile(path)
			if err != nil || string(again) != string(merged) {
				t.Fatalf("second merge changed source: %s, error = %v", again, err)
			}
		})
	}
}
