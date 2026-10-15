package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jalendport/maestro/internal/harness"
)

func TestTabArgsPerHarnessAndAccess(t *testing.T) {
	root := t.TempDir()
	projectRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	trust := fmt.Sprintf("projects={%q={trust_level=\"trusted\"}}", projectRoot)
	folder := filepath.Join(root, ".agents", "maestro", "runs", "r-test", "task")
	pointer := "Read the file `" + filepath.Join(folder, "brief.md") + "` in full, then follow it."
	readClaude := []string{"--setting-sources", "", "--permission-mode", "dontAsk", "--allowedTools",
		"Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "TodoWrite",
		"Bash(git diff *)", "Bash(git log *)", "Bash(git show *)", "Bash(git status *)", "Bash(git blame *)",
		"Bash(ls *)", "Bash(cat *)", "Bash(ls)", "Bash(git status)",
		"Write(//" + folder + "/**)", "Edit(//" + folder + "/**)",
		"mcp__solo__whoami", "mcp__solo__get_process_status", "mcp__solo__get_process_output", "mcp__solo__todo_comment_create",
		"--disallowedTools", "Edit(//" + root + "/.git/**)", "Write(//" + root + "/.git/**)"}
	editClaude := []string{"--setting-sources", "", "--permission-mode", "acceptEdits", "--allowedTools",
		"Bash(git diff *)", "Bash(git log *)", "Bash(git show *)", "Bash(git status *)", "Bash(git blame *)", "Bash(git status)", "Bash(git diff)", "Bash(ls *)", "Bash(cat *)", "WebFetch", "WebSearch",
		"Bash(go test ./...)", "Bash(go test ./... *)", "mcp__solo__*", "--add-dir", "/extra",
		"--disallowedTools", "Edit(//" + root + "/.git/**)", "Write(//" + root + "/.git/**)"}
	for _, tt := range []struct {
		name, access string
		want         []string
		wantError    bool
	}{
		{name: "claude", access: "read", want: append([]string{pointer, "--model", "model", "--effort", "high"}, readClaude...)},
		{name: "claude", access: "edit", want: append([]string{pointer, "--model", "model", "--effort", "high"}, editClaude...)},
		{name: "codex", access: "read", want: []string{pointer, "-m", "model", "-C", folder, "-c", "approval_policy=never", "-c", trust, "-c", "model_reasoning_effort=high", "-s", "workspace-write", "-c", "sandbox_workspace_write.writable_roots=[]"}},
		{name: "codex", access: "edit", want: []string{pointer, "-m", "model", "-C", root, "-c", "approval_policy=never", "-c", trust, "-c", "model_reasoning_effort=high", "-s", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--add-dir", folder, "--add-dir", "/extra", "-c", fmt.Sprintf("sandbox_workspace_write.writable_roots=[%q,\"/extra\"]", folder)}},
		// Grok runs under always-approve with the Maestro hook as the policy; a
		// read Lane writes only its Run folder, inside the project, so it runs in
		// the workspace sandbox; the edit Lane's /extra dir is outside, so it does not.
		{name: "grok", access: "read", want: []string{pointer, "--model", "model", "--no-subagents", "--cwd", root, "--reasoning-effort", "high", "--sandbox", "workspace", "--permission-mode", "bypassPermissions"}},
		{name: "grok", access: "edit", want: []string{pointer, "--model", "model", "--no-subagents", "--cwd", root, "--reasoning-effort", "high", "--permission-mode", "bypassPermissions"}},
		{name: "agy", access: "read", want: []string{"--sandbox", "--model", "model", "--effort", "high", "--prompt-interactive", pointer}},
		{name: "agy", access: "edit", want: []string{"--sandbox", "--model", "model", "--effort", "high", "--mode", "accept-edits", "--add-dir", "/extra", "--prompt-interactive", pointer}},
	} {
		t.Run(tt.name+"/"+tt.access, func(t *testing.T) {
			reg := harness.New(map[string]string{tt.name: "/bin/lane"})
			h, _ := reg.Get(tt.name)
			command, err := h.BuildTab(harness.Launch{Model: "model", Effort: "high", Access: tt.access, Root: root, RunFolder: folder,
				ReportPath: filepath.Join(folder, "result.md"), Pointer: pointer, InSolo: true, Dirs: []string{"/extra"}, AllowedCommands: []string{"go test ./..."}})
			if tt.wantError {
				if err == nil {
					t.Fatal("edit shape must be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := tabArgs(command)
			if tt.name == "claude" {
				got = got[:len(got)-2]
				wantDir := root
				if tt.access == "read" {
					wantDir = folder
				}
				if command.Cmd.Dir != wantDir {
					t.Fatalf("working directory: %s, want %s", command.Cmd.Dir, wantDir)
				}
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tab arguments:\n got %q\nwant %q", got, tt.want)
			}
			if command.ReportByText || command.PromptStdin {
				t.Fatalf("tab needs a Report file and an initial prompt: %+v", command)
			}
			if _, err := os.Stat(filepath.Join(folder, "pointer.txt")); !os.IsNotExist(err) {
				t.Fatal("tab wrote a one-shot Pointer file")
			}
		})
	}
}

// allowEach renders each rule as a Grok --allow pair.
func allowEach(rules ...string) []string {
	var out []string
	for _, r := range rules {
		out = append(out, "--allow", r)
	}
	return out
}
