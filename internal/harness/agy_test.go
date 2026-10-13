package harness

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestAgySettingsEnforcement(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		commands []string
		want     string
		reason   string
	}{
		{name: "missing", want: Enforced},
		{name: "empty", body: `{}`, want: Enforced},
		{name: "read tools", body: `{"permissions":{"allow":["read_file(*)"]}}`, want: Enforced},
		{name: "read commands", body: `{"permissions":{"allow":["command(cat)","command(ls)","command(head)","command(tail)","command(grep)","command(rg)","command(find)","command(wc)","command(pwd)","command(echo)","command(which)","command(env)","command(date)","command(git log)","command(git diff)","command(git show)","command(git status)","command(git branch)","command(git blame)","read_file(*)"]}}`, commands: []string{"cat", "ls", "head", "tail", "grep", "rg", "find", "wc", "pwd", "echo", "which", "env", "date", "git log", "git diff", "git show", "git status", "git branch", "git blame"}, want: Enforced},
		{name: "write", body: `{"permissions":{"allow":["command(rm)"]}}`, commands: []string{"rm"}, want: BestEffort, reason: "settings allow rm"},
		{name: "wildcard", body: `{"permissions":{"allow":["command(*)"]}}`, commands: []string{"*"}, want: BestEffort, reason: "settings allow *"},
		{name: "command wildcard", body: `{"permissions":{"allow":["command(cat *)"]}}`, commands: []string{"cat *"}, want: BestEffort, reason: "settings allow cat *"},
		{name: "bare git", body: `{"permissions":{"allow":["command(git)"]}}`, commands: []string{"git"}, want: BestEffort, reason: "settings allow git"},
		{name: "invalid", body: `{`, want: BestEffort},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			path := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
			if tc.body != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			reg := New(nil)
			h, _ := reg.Get("agy")
			a := h.(*Agy)
			if got := a.Enforcement("read"); got != tc.want {
				t.Fatalf("read: %s, want %s", got, tc.want)
			}
			if a.Enforcement("edit") != NoShape {
				t.Fatal("edit shape changed")
			}
			if tc.reason != "" && a.EnforcementReason() != tc.reason {
				t.Fatalf("reason: %s", a.EnforcementReason())
			}
			s := a.Settings()
			if s.Path != path || s.ReadAt.IsZero() || s.Absent != (tc.body == "") {
				t.Fatalf("settings: %+v", s)
			}
			if len(s.Commands) != 0 || len(tc.commands) != 0 {
				if !reflect.DeepEqual(s.Commands, tc.commands) {
					t.Fatalf("commands: %v, want %v", s.Commands, tc.commands)
				}
			}
		})
	}
}
