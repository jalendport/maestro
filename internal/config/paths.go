// Package config resolves where Maestro keeps its global state and loads the
// user file (~/.maestro/config.toml) and the project file (.agents/maestro.toml).
package config

import (
	"os"
	"path/filepath"
	"regexp"
)

// Home is the global state directory, ~/.maestro by default. MAESTRO_HOME
// overrides it so tests and parallel installs never share state.
func Home() string {
	if h := os.Getenv("MAESTRO_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".maestro"
	}
	return filepath.Join(home, ".maestro")
}

// Global file locations, all under Home().
func UserFile() string      { return filepath.Join(Home(), "config.toml") }
func EnvFile() string       { return filepath.Join(Home(), "env") }
func LedgerFile() string    { return filepath.Join(Home(), "ledger.jsonl") }
func BudgetFile() string    { return filepath.Join(Home(), "budget.json") }
func DiscoveryFile() string { return filepath.Join(Home(), "cache", "discovery.json") }
func RunsIndexDir() string  { return filepath.Join(Home(), "runs") }

func ControlDir(run, task string) string { return filepath.Join(Home(), "control", run, task) }

// ControlDirForFolder maps a task folder (<run>/<task>) or a Seat folder
// (<slug>/<seat> or <slug>/round-N/<seat>) to its control folder.
func ControlDirForFolder(folder string) string {
	folder = filepath.Clean(folder)
	parent := filepath.Dir(folder)
	if roundDir.MatchString(filepath.Base(parent)) {
		return filepath.Join(ControlDir(filepath.Base(filepath.Dir(parent)), filepath.Base(parent)), filepath.Base(folder))
	}
	return ControlDir(filepath.Base(parent), filepath.Base(folder))
}

var roundDir = regexp.MustCompile(`^round-[0-9]+$`)

// Per-project locations relative to the project root.
func ProjectFile(root string) string { return filepath.Join(root, ".agents", "maestro.toml") }
func RunsDir(root string) string     { return filepath.Join(root, ".agents", "maestro", "runs") }
func CouncilsDir(root string) string { return filepath.Join(root, ".agents", "maestro", "councils") }

// EnsureIgnoredDir creates an artifact folder and preserves any existing ignore rules.
func EnsureIgnoredDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString("*\n")
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// EnsureHome creates the global state directory tree.
func EnsureHome() error {
	for _, d := range []string{Home(), filepath.Join(Home(), "cache"), RunsIndexDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// FindProjectRoot walks up from dir to the nearest folder that holds .git or
// .agents/maestro.toml; the current directory is the root when neither is found. Maestro
// needs a root for the Run folder and the scope guard, not for its global verbs.
// The home directory cannot be a project root; starting there returns no root.
func FindProjectRoot(dir string) string {
	home, _ := os.UserHomeDir()
	d := dir
	for {
		if filepath.Clean(d) == filepath.Clean(home) {
			if filepath.Clean(dir) == filepath.Clean(home) {
				return ""
			}
			return dir
		}
		for _, marker := range []string{".git", filepath.Join(".agents", "maestro.toml")} {
			if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
				return d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}

func ClaudeHome(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return ExpandHome(dir)
	}
	return filepath.Join(home, ".claude")
}

func CodexHome(home string) string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return ExpandHome(dir)
	}
	return filepath.Join(home, ".codex")
}
