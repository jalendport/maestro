// Package config resolves where Maestro keeps its global state and loads the
// user file (~/.maestro/config.toml) and the project file (.agents/maestro.toml).
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// ControlDirForFolder maps an absolute Task or Seat folder to its project's
// control folder. The project key uses the root with symlinks resolved.
func ControlDirForFolder(folder string) (string, error) {
	if !filepath.IsAbs(folder) {
		return "", fmt.Errorf("control folder needs an absolute path: %s", folder)
	}
	folder = filepath.Clean(folder)
	root, artifact := filepath.Dir(folder), filepath.Base(folder)
	for parent := folder; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		kind := filepath.Base(parent)
		maestro := filepath.Dir(parent)
		agents := filepath.Dir(maestro)
		if (kind == "runs" || kind == "councils") && filepath.Base(maestro) == "maestro" && filepath.Base(agents) == ".agents" {
			root = filepath.Dir(agents)
			artifact, _ = filepath.Rel(parent, folder)
			break
		}
	}
	resolved, err := resolveControlRoot(root)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(resolved)))[:16]
	return filepath.Join(Home(), "control", key, artifact), nil
}

// A test or a removed artifact can name a root that does not exist yet.
// Resolve its existing parent before adding the missing path components.
func resolveControlRoot(root string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) || root == filepath.Dir(root) {
		return "", err
	}
	parent, err := resolveControlRoot(filepath.Dir(root))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(root)), nil
}

// IsProjectKey identifies a project folder under the global control folder.
func IsProjectKey(name string) bool {
	return len(name) == 16 && strings.Trim(name, "0123456789abcdefABCDEF") == ""
}

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
