package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeReferenceVerdicts(t *testing.T) {
	root, reference, other := t.TempDir(), t.TempDir(), t.TempDir()
	named := filepath.Join(other, "named.go")
	sibling := filepath.Join(other, "sibling.go")
	for _, path := range []string{named, sibling, filepath.Join(reference, "one.go")} {
		if err := os.WriteFile(path, []byte("package example\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "reference-link")
	if err := os.Symlink(reference, alias); err != nil {
		t.Fatal(err)
	}
	for _, access := range []string{"read", "edit"} {
		lane := &Lane{Harness: "claude", Access: access, Root: root, RunFolder: t.TempDir(), ReadDirs: []string{reference}, ReadFiles: []string{named}, Checks: []string{"go test ./...", "touch"}}
		for _, tc := range []struct {
			name, tool, cwd string
			input           map[string]any
			want            string
		}{
			{"read folder file", "Read", root, map[string]any{"file_path": filepath.Join(reference, "one.go")}, "allow"},
			{"glob folder", "Glob", root, map[string]any{"path": reference, "pattern": "**/*.go"}, "allow"},
			{"grep folder", "Grep", root, map[string]any{"path": reference, "pattern": "example"}, "allow"},
			{"named file", "Read", root, map[string]any{"file_path": named}, "allow"},
			{"grep named file", "Grep", root, map[string]any{"path": named, "pattern": "example"}, "allow"},
			{"sibling file", "Read", root, map[string]any{"file_path": sibling}, "allow"},
			{"sibling search", "Grep", root, map[string]any{"path": other}, "allow"},
			{"sibling glob", "Glob", root, map[string]any{"path": root, "pattern": filepath.Join(other, "*.go")}, "allow"},
			{"sibling list", "LS", root, map[string]any{"path": other}, "allow"},
			{"ls argument", "Bash", root, map[string]any{"command": "ls '" + reference + "'"}, "approve_read"},
			{"git directory", "Bash", root, map[string]any{"command": "git -C '" + reference + "' status"}, "approve_read"},
			{"git attached directory", "Bash", root, map[string]any{"command": "git -C'" + reference + "' status"}, "approve_read"},
			{"cat named file", "Bash", root, map[string]any{"command": "cat '" + named + "'"}, "approve_read"},
			{"cat sibling file", "Bash", root, map[string]any{"command": "cat '" + sibling + "'"}, "auto_approve"},
			{"cat relative named file", "Bash", root, map[string]any{"command": "cat '../" + filepath.Base(other) + "/named.go'"}, "approve_read"},
			{"cat relative sibling file", "Bash", root, map[string]any{"command": "cat '../" + filepath.Base(other) + "/sibling.go'"}, "auto_approve"},
			{"reference cwd", "Bash", reference, map[string]any{"command": "ls"}, "approve_read"},
			{"reference cwd grep", "Bash", reference, map[string]any{"command": "rg example ."}, "approve_read"},
			{"write command", "Bash", root, map[string]any{"command": "touch '" + reference + "/new'"}, "deny"},
			{"write cwd", "Bash", reference, map[string]any{"command": "go test ./..."}, "deny"},
			{"git write", "Bash", root, map[string]any{"command": "git -C '" + reference + "' add ."}, "deny"},
			{"git write flag", "Bash", root, map[string]any{"command": "git -C '" + reference + "' diff --output=patch"}, "deny"},
			{"find write flag", "Bash", root, map[string]any{"command": "find '" + reference + "' -delete"}, "deny"},
			{"shell redirect", "Bash", root, map[string]any{"command": "echo x > '" + named + "'"}, "deny"},
			{"unsafe environment", "Bash", reference, map[string]any{"command": "GIT_EXTERNAL_DIFF=bad git diff"}, "deny"},
			{"write tool", "Write", root, map[string]any{"file_path": filepath.Join(reference, "new.go")}, "deny"},
			{"edit named file", "Edit", root, map[string]any{"file_path": named}, "deny"},
			{"notebook edit", "NotebookEdit", root, map[string]any{"notebook_path": filepath.Join(reference, "one.ipynb")}, "deny"},
			{"symlink edit", "Edit", root, map[string]any{"file_path": filepath.Join(alias, "one.go")}, "deny"},
			{"symlink write command", "Bash", root, map[string]any{"command": "touch '" + alias + "/new.go'"}, "deny"},
		} {
			t.Run(access+"/"+tc.name, func(t *testing.T) {
				got := DecideLane(&Call{Harness: "claude", Tool: tc.tool, CWD: tc.cwd, Input: tc.input}, lane)
				if got.Kind != tc.want {
					t.Fatalf("verdict = %+v, want %s", got, tc.want)
				}
				if strings.Contains(tc.name, "sibling") {
					plain := *lane
					plain.ReadDirs, plain.ReadFiles = nil, nil
					without := DecideLane(&Call{Harness: "claude", Tool: tc.tool, CWD: tc.cwd, Input: tc.input}, &plain)
					if got != without || len(Render("claude", got)) != 0 {
						t.Fatalf("outside read changed permission flow: with=%+v without=%+v", got, without)
					}
				}
				if got.Kind == "approve_read" && !strings.Contains(string(Render("claude", got)), `"permissionDecision":"allow"`) {
					t.Fatal("reference command did not get prompt-free approval")
				}
				if got.Kind == "deny" && !strings.Contains(string(Render("claude", got)), `"permissionDecision":"deny"`) {
					t.Fatal("reference denial would continue to Relay")
				}
			})
		}
	}
}

func TestReferenceWithinWritableRootStaysReadOnly(t *testing.T) {
	root := t.TempDir()
	ref := filepath.Join(root, "reference")
	lane := &Lane{Access: "edit", Root: root, ReadDirs: []string{ref}, RunFolder: root}
	got := DecideLane(&Call{Harness: "claude", Tool: "Edit", Input: map[string]any{"file_path": filepath.Join(ref, "one.go")}}, lane)
	if got.Kind != "deny" {
		t.Fatal(got)
	}
}

func TestOtherHarnessReferenceCwd(t *testing.T) {
	root, reference := t.TempDir(), t.TempDir()
	for _, name := range []string{"grok", "agy"} {
		lane := &Lane{Harness: name, Access: "edit", Root: root, ReadDirs: []string{reference}}
		tool := "run_terminal_command"
		input := map[string]any{"command": "ls", "cwd": reference}
		want := "auto_approve"
		if name == "agy" {
			tool, input, want = "run_command", map[string]any{"CommandLine": "ls", "Cwd": reference}, "deny"
		}
		if got := DecideLane(&Call{Harness: name, Tool: tool, Input: input}, lane); got.Kind != want {
			t.Fatalf("%s: %+v, want %s", name, got, want)
		}
	}
}
