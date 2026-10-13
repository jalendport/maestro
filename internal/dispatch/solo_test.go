package dispatch

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jalendport/maestro/internal/harness"
	"time"
)

func TestPromptReady(t *testing.T) {
	since := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	const tail = "Do you want to proceed?\n1. Yes\n2. No\nEsc to cancel"
	for _, tt := range []struct {
		name         string
		hasMarker    bool
		q            string
		markerTail   string
		markerSince  time.Time
		now          time.Time
		answeredTail string
		want         bool
	}{
		{name: "marker just appeared", hasMarker: true, q: tail, markerTail: tail, markerSince: since, now: since},
		{name: "same marker after ten seconds", hasMarker: true, q: tail, markerTail: tail, markerSince: since, now: since.Add(10 * time.Second), want: true},
		{name: "answered tail", hasMarker: true, q: tail, markerTail: tail, markerSince: since, now: since.Add(10 * time.Second), answeredTail: tail},
		{name: "tail keeps changing", hasMarker: true, q: tail + "\nchanged", markerTail: tail + "\nchanged", markerSince: since.Add(9 * time.Second), now: since.Add(10 * time.Second)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := promptReady(tt.hasMarker, tt.q, tt.markerTail, tt.markerSince, tt.now, tt.answeredTail); got != tt.want {
				t.Fatalf("promptReady() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestTabArgsPerHarnessAndAccess(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, ".agents", "maestro", "runs", "r-test", "task")
	pointer := "Read the file `" + filepath.Join(folder, "brief.md") + "` in full, then follow it."
	readClaude := []string{"--setting-sources", "", "--permission-mode", "dontAsk", "--allowedTools",
		"Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "TodoWrite",
		"Bash(git diff *)", "Bash(git log *)", "Bash(git show *)", "Bash(git status *)", "Bash(git blame *)",
		"Bash(ls *)", "Bash(cat *)", "Bash(ls)", "Bash(git status)",
		"Write(//" + folder + "/**)", "Edit(//" + folder + "/**)",
		"mcp__solo__whoami", "mcp__solo__get_process_status", "mcp__solo__get_process_output", "mcp__solo__todo_comment_create"}
	editClaude := []string{"--permission-mode", "acceptEdits", "--allowedTools",
		"Bash(git diff *)", "Bash(git log *)", "Bash(git show *)", "Bash(git status *)", "Bash(git blame *)", "Bash(git status)", "Bash(git diff)", "Bash(ls *)", "Bash(cat *)", "WebFetch", "WebSearch",
		"Bash(go test ./...)", "Bash(go test ./... *)", "mcp__solo__*", "--add-dir", "/extra"}
	for _, tt := range []struct {
		name, access string
		want         []string
		wantError    bool
	}{
		{name: "claude", access: "read", want: append([]string{pointer, "--model", "model", "--effort", "high"}, readClaude...)},
		{name: "claude", access: "edit", want: append([]string{pointer, "--model", "model", "--effort", "high"}, editClaude...)},
		{name: "codex", access: "read", want: []string{pointer, "-m", "model", "-C", folder, "-c", "approval_policy=never", "-c", "model_reasoning_effort=high", "-s", "workspace-write"}},
		{name: "codex", access: "edit", want: []string{pointer, "-m", "model", "-C", root, "-c", "approval_policy=never", "-c", "model_reasoning_effort=high", "-s", "workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--add-dir", folder, "--add-dir", "/extra"}},
		{name: "grok", access: "read", want: []string{pointer, "--model", "model", "--no-subagents", "--cwd", root, "--reasoning-effort", "high", "--permission-mode", "dontAsk", "--deny", "Write", "--deny", "Edit"}},
		{name: "grok", access: "edit", want: []string{pointer, "--model", "model", "--no-subagents", "--cwd", root, "--reasoning-effort", "high", "--permission-mode", "acceptEdits", "--allow", "Bash(go test ./... *)", "--allow", "Bash(go test ./...)"}},
		{name: "agy", access: "read", want: []string{"--prompt-interactive", pointer, "--model", "model", "--effort", "high"}},
		{name: "agy", access: "edit", wantError: true},
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
			if got := tabArgs(command); !reflect.DeepEqual(got, tt.want) {
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
