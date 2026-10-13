package adapter

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("MAESTRO_HOME", filepath.Join(t.TempDir(), "maestro"))
	return home
}

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func actions(t *testing.T, r Result, states ...string) {
	t.Helper()
	if len(r.Actions) != len(states) {
		t.Fatalf("Actions = %+v, want %v", r.Actions, states)
	}
	kinds := []string{"skill", "link", "hook"}
	hasError := false
	for i, a := range r.Actions {
		if a.Kind != kinds[i] || a.State != states[i] || a.Path == "" {
			t.Fatalf("Action %d = %+v, want %s %s", i, a, kinds[i], states[i])
		}
		if a.State == "error" {
			hasError = true
			if !strings.Contains(a.Detail, a.Path) {
				t.Fatalf("error does not name path: %+v", a)
			}
		} else if a.Detail != "" {
			t.Fatalf("unexpected Detail: %+v", a)
		}
	}
	if (r.Err != nil) != hasError {
		t.Fatalf("Err = %v, has error Action = %v", r.Err, hasError)
	}
}

func TestSpecs(t *testing.T) {
	if !reflect.DeepEqual(Harnesses, []string{"claude", "codex", "grok", "agy"}) {
		t.Fatal(Harnesses)
	}
	want := []HarnessSpec{
		{".claude/skills/maestro", ".claude/settings.json", "Agent|Task|mcp__solo__spawn_agent|mcp__solo__spawn_process", 10, "A new Claude Code session loads the hook."},
		{".agents/skills/maestro", ".codex/hooks.json", "Agent|mcp__solo__spawn_agent|mcp__solo__spawn_process", 10, "Start Codex, open /hooks, and trust the Maestro PreToolUse hook."},
		{".agents/skills/maestro", ".grok/hooks/maestro.json", "spawn_subagent|Task|solo__spawn_agent|solo__spawn_process", 10, "A new Grok session binds the hook."},
		{".gemini/antigravity-cli/skills/maestro", ".gemini/config/hooks.json", "invoke_subagent", 30, "A new Antigravity session loads the hook."},
	}
	for i, name := range Harnesses {
		if got, ok := Spec(name); !ok || got != want[i] {
			t.Fatalf("Spec(%s) = %+v, %v", name, got, ok)
		}
	}
	if _, ok := Spec("unknown"); ok {
		t.Fatal("unknown Harness accepted")
	}
}

func TestInstallEachHarness(t *testing.T) {
	for _, name := range Harnesses {
		t.Run(name, func(t *testing.T) {
			home := testHome(t)
			if got := State(name, home); got.Overall != "missing" {
				t.Fatal(got)
			}
			r := Install(name, home, "shared skill\n")
			actions(t, r, "created", "created", "created")
			spec, _ := Spec(name)
			if r.Harness != name || r.Next != spec.Next {
				t.Fatal(r)
			}
			if got := State(name, home); got != (Status{name, "ok", "ok", "ok", "ok"}) {
				t.Fatal(got)
			}
			if string(read(t, SkillFile())) != "shared skill\n" {
				t.Fatal("wrong skill")
			}
			info, err := os.Stat(SkillFile())
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o644 {
				t.Fatal(info.Mode())
			}
			target, err := os.Readlink(filepath.Join(home, spec.Link))
			if err != nil {
				t.Fatal(err)
			}
			if target != SkillDir() || !filepath.IsAbs(target) {
				t.Fatal(target)
			}
			path := filepath.Join(home, spec.HookFile)
			before := read(t, path)
			var doc map[string]any
			if err := json.Unmarshal(before, &doc); err != nil {
				t.Fatal(err)
			}
			set := doc["hooks"]
			if name == "agy" {
				set = doc["maestro"]
			}
			entries := set.(map[string]any)["PreToolUse"].([]any)
			e := entries[0].(map[string]any)
			handler := e["hooks"].([]any)[0].(map[string]any)
			if len(entries) != 1 || e["matcher"] != spec.Matcher || handler["type"] != "command" || handler["command"] != "maestro hook" || handler["timeout"] != float64(spec.Timeout) {
				t.Fatal(doc)
			}
			actions(t, Install(name, home, "shared skill\n"), "unchanged", "unchanged", "unchanged")
			if !bytes.Equal(before, read(t, path)) {
				t.Fatal("repeat rewrote hook")
			}
		})
	}
}

func TestEnsureSkill(t *testing.T) {
	testHome(t)
	if changed, err := EnsureSkill("first"); !changed || err != nil {
		t.Fatal(changed, err)
	}
	oldTime := time.Unix(123456789, 0)
	if err := os.Chtimes(SkillFile(), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if changed, err := EnsureSkill("first"); changed || err != nil {
		t.Fatal(changed, err)
	}
	info, err := os.Stat(SkillFile())
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(oldTime) {
		t.Fatal("equal skill was rewritten")
	}
	if changed, err := EnsureSkill("second"); !changed || err != nil {
		t.Fatal(changed, err)
	}
	if string(read(t, SkillFile())) != "second" {
		t.Fatal("wrong updated skill")
	}
}

func TestExistingLink(t *testing.T) {
	for _, kind := range []string{"correct", "elsewhere", "directory", "file"} {
		t.Run(kind, func(t *testing.T) {
			home := testHome(t)
			spec, _ := Spec("codex")
			path := filepath.Join(home, spec.Link)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "correct":
				if err := os.Symlink(SkillDir(), path); err != nil {
					t.Fatal(err)
				}
			case "elsewhere":
				if err := os.Symlink("/some/other/skill", path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
				put(t, filepath.Join(path, "keep"), "keep")
			case "file":
				put(t, path, "keep")
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			r := Install("codex", home, "skill")
			state := "error"
			if kind == "correct" {
				state = "unchanged"
			}
			actions(t, r, "created", state, "created")
			after, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) {
				t.Fatal("link path replaced")
			}
			switch kind {
			case "elsewhere":
				target, err := os.Readlink(path)
				if err != nil || target != "/some/other/skill" {
					t.Fatal(target, err)
				}
			case "directory":
				if string(read(t, filepath.Join(path, "keep"))) != "keep" {
					t.Fatal("changed folder")
				}
			case "file":
				if string(read(t, path)) != "keep" {
					t.Fatal("changed file")
				}
			}
			if kind != "correct" && (!strings.Contains(r.Actions[1].Detail, map[string]string{"elsewhere": "symlink", "directory": "directory", "file": "file"}[kind]) || State("codex", home).Link != "wrong") {
				t.Fatal(r)
			}
		})
	}
}

func TestHookMergeOrderAndIndent(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		for _, existing := range []bool{false, true} {
			t.Run(name+map[bool]string{false: " append", true: " replace"}[existing], func(t *testing.T) {
				home := testHome(t)
				spec, _ := Spec(name)
				path := filepath.Join(home, spec.HookFile)
				old := ""
				if existing {
					old = `,{"hooks":[{"command":"maestro hook codex","type":"command","timeout":99}],"matcher":"old"}`
				}
				source := "{\n    \"z\": {\"b\": 1, \"a\": 2},\n    \"hooks\": {\"Stop\": [\"keep\"], \"PreToolUse\": [{\"matcher\":\"first\",\"hooks\":[{\"command\":\"keep first\",\"type\":\"command\"}]}" + old + ", {\"matcher\":\"last\",\"hooks\":[{\"command\":\"keep last\",\"type\":\"command\"}]}], \"PostToolUse\": []},\n    \"a\": true\n}\n"
				put(t, path, source)
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
				state := "created"
				if existing {
					state = "updated"
				}
				actions(t, Install(name, home, "skill"), "created", "created", state)
				output := read(t, path)
				if !bytes.HasPrefix(output, []byte("{\n    \"z\":")) {
					t.Fatalf("indent changed: %s", output)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0o600 {
					t.Fatal(info, err)
				}
				before, err := decode([]byte(source))
				if err != nil {
					t.Fatal(err)
				}
				after, err := decode(output)
				if err != nil {
					t.Fatal(err)
				}
				keys := func(v *value) []string {
					var keys []string
					for _, m := range v.object {
						keys = append(keys, m.key)
					}
					return keys
				}
				if !reflect.DeepEqual(keys(before), keys(after)) || !reflect.DeepEqual(keys(before.get("hooks")), keys(after.get("hooks"))) || !reflect.DeepEqual(keys(after.get("z")), []string{"b", "a"}) {
					t.Fatalf("key order changed: %s", output)
				}
				for _, k := range []string{"z", "a"} {
					if !equal(before.get(k), after.get(k)) {
						t.Fatal("changed", k)
					}
				}
				for _, k := range []string{"Stop", "PostToolUse"} {
					if !equal(before.get("hooks").get(k), after.get("hooks").get(k)) {
						t.Fatal("changed", k)
					}
				}
				oldEntries := before.get("hooks").get("PreToolUse").array
				newEntries := after.get("hooks").get("PreToolUse").array
				last := 1
				if existing {
					last = 2
				}
				if !bytes.Equal(mustJSON(t, oldEntries[0]), mustJSON(t, newEntries[0])) || !bytes.Equal(mustJSON(t, oldEntries[last]), mustJSON(t, newEntries[last])) {
					t.Fatal("other handlers changed")
				}
				desiredIndex := 2
				if existing {
					desiredIndex = 1
				}
				if !equal(newEntries[desiredIndex], entry(spec)) {
					t.Fatal("wrong Maestro entry")
				}
				if State(name, home).Overall != "ok" {
					t.Fatal(State(name, home))
				}
				actions(t, Install(name, home, "skill"), "unchanged", "unchanged", "unchanged")
				if !bytes.Equal(output, read(t, path)) {
					t.Fatal("unchanged file rewritten")
				}
			})
		}
	}
}

func mustJSON(t *testing.T, v *value) []byte {
	t.Helper()
	b, err := v.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHookMissingKeys(t *testing.T) {
	for _, source := range []string{`{"other":true}`, `{"hooks":{"Stop":[]},"other":true}`} {
		t.Run(source, func(t *testing.T) {
			home := testHome(t)
			spec, _ := Spec("claude")
			path := filepath.Join(home, spec.HookFile)
			put(t, path, source)
			if State("claude", home).Hook != "missing" {
				t.Fatal(State("claude", home))
			}
			actions(t, Install("claude", home, "skill"), "created", "created", "created")
			if !strings.Contains(string(read(t, path)), `"other": true`) || State("claude", home).Hook != "ok" {
				t.Fatal(string(read(t, path)))
			}
		})
	}
}

func TestHookFileSymlinkAndEscapes(t *testing.T) {
	home := testHome(t)
	spec, _ := Spec("claude")
	path := filepath.Join(home, spec.HookFile)
	real := filepath.Join(t.TempDir(), "dotfiles", "settings.json")
	put(t, real, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"a && b > c <d>"}]}]}}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	actions(t, Install("claude", home, "skill"), "created", "created", "created")
	if target, err := os.Readlink(path); err != nil || target != real {
		t.Fatal("hook symlink replaced", target, err)
	}
	output := string(read(t, real))
	if !strings.Contains(output, `"a && b > c <d>"`) || !strings.Contains(output, `"maestro hook"`) {
		t.Fatal(output)
	}
	if State("claude", home).Hook != "ok" {
		t.Fatal(State("claude", home))
	}
}

func TestOwnedHookSets(t *testing.T) {
	for _, name := range []string{"grok", "agy"} {
		t.Run(name, func(t *testing.T) {
			home := testHome(t)
			spec, _ := Spec(name)
			path := filepath.Join(home, spec.HookFile)
			source := `{"before":{"z":1,"a":2},"maestro":{"PreToolUse":[]},"after":true}`
			put(t, path, source)
			actions(t, Install(name, home, "skill"), "created", "created", "updated")
			root, err := decode(read(t, path))
			if err != nil {
				t.Fatal(err)
			}
			if name == "grok" {
				if !equal(root, object(member{"hooks", hookSet(spec)})) {
					t.Fatal(string(read(t, path)))
				}
			} else {
				if len(root.object) != 3 || root.object[0].key != "before" || root.object[1].key != "maestro" || root.object[2].key != "after" {
					t.Fatal(string(read(t, path)))
				}
				before, _ := decode([]byte(source))
				if !bytes.Equal(mustJSON(t, before.get("before")), mustJSON(t, root.get("before"))) || !equal(before.get("after"), root.get("after")) {
					t.Fatal("other sets changed")
				}
			}
			actions(t, Install(name, home, "skill"), "unchanged", "unchanged", "unchanged")
		})
	}
}

func TestInvalidJSON(t *testing.T) {
	for _, name := range Harnesses {
		for _, source := range []string{`{"hooks":`, `{} {}`, `{"a":1,}`, ""} {
			t.Run(name+source, func(t *testing.T) {
				home := testHome(t)
				spec, _ := Spec(name)
				path := filepath.Join(home, spec.HookFile)
				put(t, path, source)
				r := Install(name, home, "skill")
				actions(t, r, "created", "created", "error")
				if !strings.Contains(r.Actions[2].Detail, "parse JSON") {
					t.Fatal(r)
				}
				if !bytes.Equal([]byte(source), read(t, path)) {
					t.Fatal("invalid file changed")
				}
				got := State(name, home)
				if got.Hook != "wrong" || got.Overall != "partial" {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestStateAndContinueAfterSkillError(t *testing.T) {
	home := testHome(t)
	if err := os.MkdirAll(SkillFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	actions(t, Install("claude", home, "skill"), "error", "created", "created")
	if err := os.Remove(SkillFile()); err != nil {
		t.Fatal(err)
	}
	s := State("claude", home)
	if s.Skill != "missing" || s.Link != "ok" || s.Hook != "ok" || s.Overall != "partial" {
		t.Fatal(s)
	}
	actions(t, Install("claude", home, "first"), "created", "unchanged", "unchanged")
	actions(t, Install("claude", home, "second"), "updated", "unchanged", "unchanged")
	spec, _ := Spec("claude")
	path := filepath.Join(home, spec.HookFile)
	put(t, path, `{"hooks":{"PreToolUse":[{"matcher":"wrong","hooks":[{"command":"maestro hook"}]}]}}`)
	if s := State("claude", home); s.Hook != "wrong" || s.Overall != "partial" {
		t.Fatal(s)
	}
}

func TestMalformedHookShape(t *testing.T) {
	for _, source := range []string{`[]`, `{"hooks":null}`, `{"hooks":{"PreToolUse":{}}}`} {
		t.Run(source, func(t *testing.T) {
			home := testHome(t)
			spec, _ := Spec("codex")
			path := filepath.Join(home, spec.HookFile)
			put(t, path, source)
			actions(t, Install("codex", home, "skill"), "created", "created", "error")
			if string(read(t, path)) != source || State("codex", home).Hook != "wrong" {
				t.Fatal(string(read(t, path)))
			}
		})
	}
}

func TestNamedHookState(t *testing.T) {
	for _, tt := range []struct{ source, want string }{
		{`{"maestro":{}}`, "missing"},
		{`{"maestro":{"PreToolUse":[]}}`, "missing"},
		{`{"maestro":null}`, "wrong"},
		{`{"maestro":{"PreToolUse":[{"matcher":"wrong","hooks":[{"command":"maestro hook"}]}]}}`, "wrong"},
	} {
		t.Run(tt.source, func(t *testing.T) {
			home := testHome(t)
			spec, _ := Spec("agy")
			path := filepath.Join(home, spec.HookFile)
			put(t, path, tt.source)
			if got := State("agy", home); got.Hook != tt.want {
				t.Fatal(got)
			}
		})
	}
}

func TestInstallAndStateUseHarnessConfigLocations(t *testing.T) {
	for _, tc := range []struct{ harness, variable, hook, link string }{
		{"claude", "CLAUDE_CONFIG_DIR", "settings.json", "skills/maestro"},
		{"codex", "CODEX_HOME", "hooks.json", ""},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			home := testHome(t)
			dir := filepath.Join(t.TempDir(), "config with spaces")
			t.Setenv(tc.variable, dir)
			result := Install(tc.harness, home, "test skill")
			if result.Err != nil {
				t.Fatal(result.Err)
			}
			if result.Actions[2].Path != filepath.Join(dir, tc.hook) {
				t.Fatalf("hook path: %+v", result.Actions[2])
			}
			if tc.link != "" && result.Actions[1].Path != filepath.Join(dir, tc.link) {
				t.Fatalf("link path: %+v", result.Actions[1])
			}
			if got := State(tc.harness, home); got.Overall != "ok" {
				t.Fatalf("state: %+v", got)
			}
			spec, _ := Spec(tc.harness)
			if _, err := os.Stat(filepath.Join(home, spec.HookFile)); !os.IsNotExist(err) {
				t.Fatalf("wrote default hook path: %v", err)
			}
		})
	}
}
