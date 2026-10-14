// Package adapter installs the shared skill and each Harness's link and hook.
package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jalendport/maestro/internal/config"
)

var Harnesses = []string{"claude", "codex", "grok", "agy"}

const Command = "maestro hook"

// HarnessSpec holds paths relative to the Harness home and its hook facts.
// Go cannot give both this type and the Spec function the same name.
type HarnessSpec struct {
	Link     string
	HookFile string
	Matcher  string
	Timeout  int
	Next     string
}

func Spec(harness string) (HarnessSpec, bool) {
	switch harness {
	case "claude":
		return HarnessSpec{".claude/skills/maestro", ".claude/settings.json", "Agent|Task|mcp__solo__spawn_agent|mcp__solo__spawn_process", 10, "A new Claude Code session loads the hook."}, true
	case "codex":
		return HarnessSpec{".agents/skills/maestro", ".codex/hooks.json", "Agent|mcp__solo__spawn_agent|mcp__solo__spawn_process", 10, "Start Codex, open /hooks, and trust the Maestro PreToolUse hook."}, true
	case "grok":
		return HarnessSpec{".agents/skills/maestro", ".grok/hooks/maestro.json", "spawn_subagent|Task|solo__spawn_agent|solo__spawn_process", 10, "A new Grok session binds the hook."}, true
	case "agy":
		return HarnessSpec{".gemini/antigravity-cli/skills/maestro", ".gemini/config/hooks.json", "run_command|write_to_file|replace_file_content|multi_replace_file_content|invoke_subagent|define_subagent|manage_subagents", 660, "A new Antigravity session loads the hook."}, true
	}
	return HarnessSpec{}, false
}

// Paths resolves a Harness's files for install, status, and printed steps.
func Paths(harness, home string) (HarnessSpec, bool) {
	spec, ok := Spec(harness)
	if !ok {
		return spec, false
	}
	spec.Link = filepath.Join(home, spec.Link)
	spec.HookFile = filepath.Join(home, spec.HookFile)
	switch harness {
	case "claude":
		spec.Link = filepath.Join(config.ClaudeHome(home), "skills", "maestro")
		spec.HookFile = filepath.Join(config.ClaudeHome(home), "settings.json")
	case "codex":
		spec.HookFile = filepath.Join(config.CodexHome(home), "hooks.json")
	}
	return spec, true
}

func SkillDir() string  { return filepath.Join(config.Home(), "skills", "maestro") }
func SkillFile() string { return filepath.Join(SkillDir(), "SKILL.md") }

func EnsureSkill(content string) (bool, error) {
	b, err := os.ReadFile(SkillFile())
	if err == nil && string(b) == content {
		return false, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := atomicWrite(SkillFile(), []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

type Action struct {
	Kind   string
	Path   string
	State  string
	Detail string
}

type Result struct {
	Harness string
	Actions []Action
	Next    string
	Err     error
}

func Install(harness string, home string, skill string) Result {
	r := Result{Harness: harness}
	spec, ok := Paths(harness, home)
	if !ok {
		r.Err = fmt.Errorf("unknown harness %q", harness)
		return r
	}
	r.Next = spec.Next
	record := func(kind, path, state string, err error) {
		a := Action{Kind: kind, Path: path, State: state}
		if err != nil {
			a.State = "error"
			a.Detail = fmt.Sprintf("%s: %s", path, err)
			r.Err = errors.Join(r.Err, errors.New(a.Detail))
		}
		r.Actions = append(r.Actions, a)
	}
	_, statErr := os.Stat(SkillFile())
	changed, err := EnsureSkill(skill)
	state := "unchanged"
	if changed {
		state = "updated"
		if errors.Is(statErr, os.ErrNotExist) {
			state = "created"
		}
	}
	record("skill", SkillFile(), state, err)
	link := spec.Link
	state, err = ensureLink(link)
	record("link", link, state, err)
	path := spec.HookFile
	state, err = ensureHook(harness, path, spec)
	record("hook", path, state, err)
	return r
}

func skillTarget() (string, error) { return filepath.Abs(SkillDir()) }

func ensureLink(path string) (string, error) {
	target, err := skillTarget()
	if err != nil {
		return "error", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "error", err
		}
		if err := os.Symlink(target, path); err != nil {
			return "error", err
		}
		return "created", nil
	}
	if err != nil {
		return "error", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		actual, err := os.Readlink(path)
		if err != nil {
			return "error", err
		}
		if actual == target {
			return "unchanged", nil
		}
		return "error", fmt.Errorf("symlink to %q already exists", actual)
	}
	kind := "file"
	if info.IsDir() {
		kind = "directory"
	}
	return "error", fmt.Errorf("%s already exists", kind)
}

type Status struct {
	Harness string
	Skill   string
	Link    string
	Hook    string
	Overall string
}

func State(harness string, home string) Status {
	s := Status{Harness: harness, Skill: "missing", Link: "missing", Hook: "missing"}
	if _, err := os.Stat(SkillFile()); err == nil {
		s.Skill = "ok"
	} else if !errors.Is(err, os.ErrNotExist) {
		s.Skill = "wrong"
	}
	spec, ok := Paths(harness, home)
	if !ok {
		s.Link, s.Hook = "wrong", "wrong"
	} else {
		path := spec.Link
		if _, err := os.Lstat(path); err == nil {
			s.Link = "wrong"
			actual, err := os.Readlink(path)
			target, targetErr := skillTarget()
			if err == nil && targetErr == nil && actual == target {
				s.Link = "ok"
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			s.Link = "wrong"
		}
		s.Hook = hookState(harness, spec.HookFile, spec)
	}
	s.Overall = "partial"
	if s.Skill == "ok" && s.Link == "ok" && s.Hook == "ok" {
		s.Overall = "ok"
	}
	if s.Skill == "missing" && s.Link == "missing" && s.Hook == "missing" {
		s.Overall = "missing"
	}
	return s
}

// atomicWrite keeps the old file in place until all new bytes are written.
// A symlinked file is written at its target, so the symlink itself stays.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".maestro-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
