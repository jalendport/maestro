package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecideLaneWorktreeGitDirectoryWrites(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	project := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", project}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "Start")
	root := filepath.Join(t.TempDir(), "worktree")
	git("worktree", "add", "-b", "task", root, "HEAD")
	data, err := os.ReadFile(filepath.Join(root, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir: "))
	for _, relative := range []bool{false, true} {
		pointer := gitdir
		if relative {
			pointer, err = filepath.Rel(root, gitdir)
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+pointer+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		lane := &Lane{Access: "edit", Root: root, RunFolder: filepath.Join(project, ".agents", "run"), Dirs: []string{project}}
		for _, target := range []string{".git", filepath.Join(root, ".git"), gitdir, filepath.Join(gitdir, "HEAD"), filepath.Join(gitdir, "new-file")} {
			call := &Call{Tool: "Write", CWD: root, Input: map[string]any{"file_path": target}}
			if got := DecideLane(call, lane); got.Kind != "deny" || !strings.Contains(got.Reason, ".git") {
				t.Fatalf("relative=%v target=%s: %+v", relative, target, got)
			}
		}
		for _, target := range []string{"source.go", filepath.Join(lane.RunFolder, "result.md")} {
			call := &Call{Tool: "Write", CWD: root, Input: map[string]any{"file_path": target}}
			if got := DecideLane(call, lane); got.Kind != "auto_approve" {
				t.Fatalf("target=%s: %+v", target, got)
			}
		}
	}
}
