package adapter

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravityDisabledFixture(t *testing.T) {
	for _, location := range []string{"all", "maestro", "PreToolUse", "PreInvocation", "handler"} {
		t.Run(location, func(t *testing.T) {
			home := testHome(t)
			spec, _ := Paths("agy", home)
			root, err := decode(read(t, "testdata/agy-disabled.json"))
			if err != nil {
				t.Fatal(err)
			}
			set := root.get("maestro")
			if location != "all" {
				removeDisabledFields(set)
				target := set
				if location == "handler" {
					target = set.get("PreToolUse").array[0].get("hooks").array[0]
				} else if location != "maestro" {
					target = set.get(location).array[0]
				}
				target.set("enabled", scalar(false))
			}
			put(t, spec.HookFile, string(mustJSON(t, root)))
			state := State("agy", home)
			if state.Hook != "disabled" || !strings.Contains(state.HookReason, spec.HookFile) || !strings.Contains(state.HookReason, "enabled: false") || !strings.Contains(state.HookReason, "maestro adapter install agy") {
				t.Fatal(state)
			}
			if result := Install("agy", home, "skill"); result.Err != nil {
				t.Fatal(result.Err)
			}
			state = State("agy", home)
			if state.Overall != "ok" || state.HookReason != "" {
				t.Fatal(state)
			}
			after, err := decode(read(t, spec.HookFile))
			if err != nil || hasDisabledField(after.get("maestro")) || !equal(root.get("other"), after.get("other")) {
				t.Fatalf("wrong install: %s, %v", read(t, spec.HookFile), err)
			}
			actions(t, Install("agy", home, "skill"), "unchanged", "unchanged", "unchanged", "unchanged")
		})
	}
}

func TestGrokDisabledHookRecords(t *testing.T) {
	for _, record := range []string{"maestro.json", "maestro", "absolute", "# maestro.json", "other.json", "", "maestro.json.extra"} {
		t.Run(record, func(t *testing.T) {
			home := testHome(t)
			if result := Install("grok", home, "skill"); result.Err != nil {
				t.Fatal(result.Err)
			}
			spec, _ := Paths("grok", home)
			line := record
			if record == "absolute" {
				line = spec.HookFile
			}
			path := filepath.Join(os.Getenv("GROK_HOME"), "disabled-hooks")
			content := "# Disabled user hooks\n\n  " + line + "  \r\nother.json\n"
			put(t, path, content)
			want := "ok"
			if record == "maestro.json" || record == "maestro" || record == "absolute" {
				want = "disabled"
			}
			state := State("grok", home)
			if state.Hook != want {
				t.Fatal(state)
			}
			if want == "disabled" && (state.HookReason != path+" lists the Maestro hook; remove that line" || state.Overall != "partial") {
				t.Fatal(state)
			}
			actions(t, Install("grok", home, "skill"), "unchanged", "unchanged", "unchanged", "unchanged")
			if string(read(t, path)) != content || State("grok", home).Hook != want {
				t.Fatal("install changed the disable control")
			}
		})
	}
}

func TestGrokManagedHookPolicy(t *testing.T) {
	for _, source := range []string{"user", "system"} {
		for _, file := range []string{"requirements.toml", "managed_config.toml"} {
			for _, key := range []string{"allow_managed_hooks_only", "allowManagedHooksOnly"} {
				for _, value := range []string{"false", "true", `"false"`, "0", "[]", "{}"} {
					t.Run(fmt.Sprintf("%s/%s/%s/%s", source, file, key, value), func(t *testing.T) {
						home := testHome(t)
						if result := Install("grok", home, "skill"); result.Err != nil {
							t.Fatal(result.Err)
						}
						dir := os.Getenv("GROK_HOME")
						if source == "system" {
							dir = grokManagedDir
						}
						path := filepath.Join(dir, file)
						content := key + " = " + value + "\n"
						put(t, path, content)
						// A false value in another layer cannot release this pin.
						other := "requirements.toml"
						if file == other {
							other = "managed_config.toml"
						}
						put(t, filepath.Join(dir, other), key+" = false\n")
						state := State("grok", home)
						want := "disabled"
						if value == "false" {
							want = "ok"
						}
						if state.Hook != want || (want == "disabled" && state.HookReason != key+" in "+path+" skips user hooks") {
							t.Fatal(state)
						}
						actions(t, Install("grok", home, "skill"), "unchanged", "unchanged", "unchanged", "unchanged")
						if !bytes.Equal(read(t, path), []byte(content)) || State("grok", home).Hook != want {
							t.Fatal("install changed the policy")
						}
					})
				}
			}
		}
	}
}

func TestHookStateRequiresFullDedicatedEntry(t *testing.T) {
	for _, name := range Harnesses {
		for _, change := range []string{"matcher", "shared", "extra", "duplicate"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				home := testHome(t)
				spec, _ := Paths(name, home)
				set := hookSet(spec)
				entries := set.get("PreToolUse")
				item := entries.array[0]
				switch change {
				case "matcher":
					item.set("matcher", scalar("spawn_only"))
				case "shared":
					handlers := item.get("hooks")
					handlers.array = append(handlers.array, object(member{"command", scalar("foreign")}))
				case "extra":
					item.set("extra", scalar(true))
				case "duplicate":
					entries.array = append(entries.array, entry(spec, spec.Events[0]))
				}
				key := "hooks"
				if name == "agy" {
					key = "maestro"
				}
				put(t, spec.HookFile, string(mustJSON(t, object(member{key, set}))))
				if state := State(name, home); state.Hook != "wrong" {
					t.Fatal(state)
				}
				if result := Install(name, home, "skill"); result.Err != nil {
					t.Fatal(result.Err)
				}
				if state := State(name, home); state.Overall != "ok" {
					t.Fatal(state)
				}
			})
		}
	}
}

func TestCodexInstallRewritesBareCommands(t *testing.T) {
	home := testHome(t)
	spec, _ := Paths("codex", home)
	set := hookSet(spec)
	for _, event := range spec.Events {
		set.get(event.Name).array[0].get("hooks").array[0].set("command", scalar(Command))
	}
	put(t, spec.HookFile, string(mustJSON(t, object(member{"hooks", set}))))
	if state := State("codex", home); state.Hook != "wrong" || state.Reminder != "wrong" {
		t.Fatal(state)
	}
	actions(t, Install("codex", home, "skill"), "created", "created", "updated", "updated")
	if state := State("codex", home); state.Overall != "ok" {
		t.Fatal(state)
	}
}

func TestGrokDefaultHome(t *testing.T) {
	home := testHome(t)
	t.Setenv("GROK_HOME", "")
	spec, _ := Paths("grok", home)
	if spec.HookFile != filepath.Join(home, ".grok", "hooks", "maestro.json") || ReminderFile(home) != filepath.Join(home, ".grok", "rules", "maestro.md") {
		t.Fatal(spec.HookFile, ReminderFile(home))
	}
}
