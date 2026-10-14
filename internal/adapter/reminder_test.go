package adapter

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
)

func TestReminderText(t *testing.T) {
	want := "Maestro: you are the Orchestrator. You plan, judge, and read every diff; Lanes write the code. When the user approves a plan, write the Plan file and run `maestro run`. One bounded task is a Brief file and `maestro dispatch`. A review, or one question for several minds, is `maestro council`. The maestro skill holds the rules; `maestro help brief` and `maestro help plan` print the formats. If your prompt points you at a Maestro Brief file, you are a Lane, not the Orchestrator: ignore this and follow the Brief."
	if Reminder != want {
		t.Fatalf("Reminder = %q", Reminder)
	}
}

func TestReminderHookInstallAndState(t *testing.T) {
	for _, tc := range []struct{ harness, key, event, want string }{
		{"claude", "hooks", "UserPromptSubmit", `[{"hooks":[{"type":"command","command":"maestro hook","timeout":10}]}]`},
		{"codex", "hooks", "UserPromptSubmit", `[{"hooks":[{"type":"command","command":"maestro hook","timeout":10}]}]`},
		{"agy", "maestro", "PreInvocation", `[{"type":"command","command":"maestro hook agy","timeout":30}]`},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			home := testHome(t)
			r := Install(tc.harness, home, "skill")
			actions(t, r, "created", "created", "created", "created")
			spec, _ := Paths(tc.harness, home)
			if r.Actions[3].Path != spec.HookFile {
				t.Fatal(r.Actions[3])
			}
			root, err := decode(read(t, spec.HookFile))
			if err != nil {
				t.Fatal(err)
			}
			want, err := decode([]byte(tc.want))
			if err != nil || !equal(root.get(tc.key).get(tc.event), want) {
				t.Fatalf("Carrier = %s, want %s: %v", read(t, spec.HookFile), tc.want, err)
			}
			for _, state := range []string{"missing", "wrong", "ok"} {
				set := hookSet(spec)
				set.object = set.object[:1]
				if state == "wrong" {
					set.set(tc.event, array(object(member{"command", scalar("maestro hook old")})))
					if tc.harness != "agy" {
						set.set(tc.event, array(object(member{"hooks", array(object(member{"command", scalar("maestro hook old")}))})))
					}
				} else if state == "ok" {
					set.set(tc.event, want)
				}
				output, err := encode(object(member{tc.key, set}), "  ")
				if err != nil {
					t.Fatal(err)
				}
				put(t, spec.HookFile, string(output))
				got := State(tc.harness, home)
				overall := "partial"
				if state == "ok" {
					overall = "ok"
				}
				if got.Hook != "ok" || got.Reminder != state || got.Overall != overall {
					t.Fatalf("state %s: %+v", state, got)
				}
			}
		})
	}
}

func TestReminderHookMergePreservesForeignEntries(t *testing.T) {
	for _, name := range []string{"claude", "codex", "agy"} {
		for _, existing := range []bool{false, true} {
			t.Run(name+map[bool]string{false: " append", true: " replace"}[existing], func(t *testing.T) {
				home := testHome(t)
				spec, _ := Paths(name, home)
				event := spec.Events[1]
				key := "hooks"
				first := `{"extra":{"z":1,"a":2},"hooks":[{"command":"foreign first","type":"command"}]}`
				last := `{"hooks":[{"command":"foreign last","type":"command"}],"extra":true}`
				old := `,{"hooks":[{"command":"maestro hook old","type":"command","timeout":99}]}`
				if name == "agy" {
					key = "maestro"
					first = `{"extra":{"z":1,"a":2},"command":"foreign first","type":"command"}`
					last = `{"command":"foreign last","type":"command","extra":true}`
					old = `,{"command":"maestro hook old","type":"command","timeout":99}`
				}
				if !existing {
					old = ""
				}
				source := `{"before":true,"` + key + `":{"Stop":[],"` + event.Name + `":[` + first + old + `,` + last + `],"After":{}},"after":true}`
				put(t, spec.HookFile, source)
				state := "created"
				if existing {
					state = "updated"
				}
				actions(t, Install(name, home, "skill"), "created", "created", "created", state)
				root, err := decode(read(t, spec.HookFile))
				if err != nil {
					t.Fatal(err)
				}
				set := root.get(key)
				if len(set.object) != 4 || set.object[0].key != "Stop" || set.object[1].key != event.Name || set.object[2].key != "After" || set.object[3].key != "PreToolUse" {
					t.Fatalf("event key order: %s", read(t, spec.HookFile))
				}
				entries := set.get(event.Name).array
				foreignFirst, _ := decode([]byte(first))
				foreignLast, _ := decode([]byte(last))
				lastIndex, desiredIndex := 1, 2
				if existing {
					lastIndex, desiredIndex = 2, 1
				}
				if len(entries) != 3 || !bytes.Equal(mustJSON(t, entries[0]), mustJSON(t, foreignFirst)) || !bytes.Equal(mustJSON(t, entries[lastIndex]), mustJSON(t, foreignLast)) || !equal(entries[desiredIndex], entry(spec, event)) {
					t.Fatalf("Reminder entries: %s", read(t, spec.HookFile))
				}
				if got := State(name, home); got.Reminder != "ok" || got.Overall != "ok" {
					t.Fatal(got)
				}
				before := read(t, spec.HookFile)
				actions(t, Install(name, home, "skill"), "unchanged", "unchanged", "unchanged", "unchanged")
				if !bytes.Equal(before, read(t, spec.HookFile)) {
					t.Fatal("repeat changed hook file")
				}
			})
		}
	}
}

func TestMalformedEventDoesNotWriteEitherEvent(t *testing.T) {
	for _, name := range []string{"claude", "codex", "agy"} {
		for _, malformed := range []string{"PreToolUse", "reminder"} {
			t.Run(name+" "+malformed, func(t *testing.T) {
				home := testHome(t)
				spec, _ := Paths(name, home)
				key := "hooks"
				if name == "agy" {
					key = "maestro"
				}
				event := malformed
				if event == "reminder" {
					event = spec.Events[1].Name
				}
				source := `{"` + key + `":{"` + event + `":{}}}`
				put(t, spec.HookFile, source)
				actions(t, Install(name, home, "skill"), "created", "created", "error", "error")
				if string(read(t, spec.HookFile)) != source {
					t.Fatal("malformed file changed")
				}
				got := State(name, home)
				if event == "PreToolUse" && (got.Hook != "wrong" || got.Reminder != "missing") || event != "PreToolUse" && (got.Hook != "missing" || got.Reminder != "wrong") {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestGrokReminderFile(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, ".grok", "rules", "maestro.md")
	agents := filepath.Join(home, ".grok", "AGENTS.md")
	put(t, agents, "keep Grok instructions\n")
	result := Install("grok", home, "skill")
	actions(t, result, "created", "created", "created", "created")
	if result.Actions[3].Path != path {
		t.Fatal(result.Actions[3])
	}
	want := "# Maestro\n\nThis rule applies inside a project that holds `.agents/maestro.toml`, or everywhere when `[hook] scope = \"all\"` in `" + config.UserFile() + "`. Elsewhere, ignore it. If your prompt points you at a Maestro Brief file, you are a Lane, not the Orchestrator: ignore this rule and follow the Brief.\n\n" + Reminder + "\n"
	if string(read(t, path)) != want {
		t.Fatalf("rules file = %q", read(t, path))
	}
	oldTime := time.Unix(123456789, 0)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	actions(t, Install("grok", home, "skill"), "unchanged", "unchanged", "unchanged", "unchanged")
	info, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(oldTime) || info.Mode().Perm() != 0o644 {
		t.Fatal(info, err)
	}
	put(t, path, "old Reminder")
	if got := State("grok", home); got.Reminder != "wrong" || got.Overall != "partial" {
		t.Fatal(got)
	}
	actions(t, Install("grok", home, "skill"), "unchanged", "unchanged", "unchanged", "updated")
	if string(read(t, path)) != want {
		t.Fatal("wrong replacement")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := State("grok", home); got.Hook != "ok" || got.Reminder != "missing" || got.Overall != "partial" {
		t.Fatal(got)
	}
	if string(read(t, agents)) != "keep Grok instructions\n" {
		t.Fatal("Grok instructions changed")
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	actions(t, Install("grok", home, "skill"), "unchanged", "unchanged", "unchanged", "error")
	if got := State("grok", home); got.Reminder != "wrong" {
		t.Fatal(got)
	}
}

func TestGrokReminderTracksMaestroHome(t *testing.T) {
	home := testHome(t)
	path := ReminderFile(home)
	for _, maestroHome := range []string{"", filepath.Join(t.TempDir(), "home with spaces")} {
		t.Setenv("MAESTRO_HOME", maestroHome)
		want := "~/.maestro/config.toml"
		if maestroHome != "" {
			want = filepath.Join(maestroHome, "config.toml")
		}
		if !strings.Contains(ReminderRule(), "`"+want+"`") {
			t.Fatal(ReminderRule())
		}
		if _, err := ensureReminder(path); err != nil {
			t.Fatal(err)
		}
		if reminderState(path) != "ok" || string(read(t, path)) != ReminderRule() {
			t.Fatal("rules file does not match current home")
		}
	}
	t.Setenv("MAESTRO_HOME", t.TempDir())
	if reminderState(path) != "wrong" {
		t.Fatal("rules file with old config path accepted")
	}
}

func TestReminderKeepsForeignHandlersInSharedEntry(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		t.Run(name, func(t *testing.T) {
			home := testHome(t)
			spec, _ := Paths(name, home)
			source := `{"hooks":{"UserPromptSubmit":[{"extra":{"z":1,"a":2},"hooks":[{"command":"before","type":"command"},{"command":"maestro hook old","timeout":99},{"command":"after","type":"command"}]}]}}`
			put(t, spec.HookFile, source)
			actions(t, Install(name, home, "skill"), "created", "created", "created", "updated")
			before, _ := decode([]byte(source))
			after, err := decode(read(t, spec.HookFile))
			if err != nil {
				t.Fatal(err)
			}
			oldEntry := before.get("hooks").get("UserPromptSubmit").array[0]
			newEntry := after.get("hooks").get("UserPromptSubmit").array[0]
			oldHandlers, newHandlers := oldEntry.get("hooks").array, newEntry.get("hooks").array
			if len(newHandlers) != 3 || !bytes.Equal(mustJSON(t, oldHandlers[0]), mustJSON(t, newHandlers[0])) || !bytes.Equal(mustJSON(t, oldHandlers[2]), mustJSON(t, newHandlers[2])) || !bytes.Equal(mustJSON(t, oldEntry.get("extra")), mustJSON(t, newEntry.get("extra"))) {
				t.Fatalf("foreign handlers changed: %s", read(t, spec.HookFile))
			}
			if got := State(name, home); got.Reminder != "ok" {
				t.Fatal(got)
			}
			if strings.Contains(string(mustJSON(t, newEntry)), "matcher") {
				t.Fatal("Reminder has a matcher")
			}
			actions(t, Install(name, home, "skill"), "unchanged", "unchanged", "unchanged", "unchanged")
		})
	}
}
