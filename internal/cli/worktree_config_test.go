package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/router"
)

func worktreeConfigFixture(t *testing.T) (string, func(...string) string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "Test User")
	t.Setenv("GIT_AUTHOR_EMAIL", "user@example.test")
	t.Setenv("GIT_COMMITTER_NAME", "Test User")
	t.Setenv("GIT_COMMITTER_EMAIL", "user@example.test")
	bin := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	root := t.TempDir()
	t.Chdir(root)
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-b", "master")
	writeWorktreeConfig(t, filepath.Join(root, ".gitignore"), ".agents/\nvendor/\nnode_modules/\n")
	writeWorktreeConfig(t, filepath.Join(root, "base.txt"), "base\n")
	git("add", ".")
	git("commit", "-m", "Start test repository")
	return root, git
}

func writeWorktreeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeProjectSettings(t *testing.T) {
	for _, tc := range []struct {
		name, user, project, message string
		on                           bool
	}{
		{"defaults", "", "", "Add probe\n\nCo-Authored-By: Codex <noreply@openai.com>", true},
		{"global", "[run]\nworktrees = false\n[commit]\nmessage = \"global: {title} {lane}\"\n[commit.identities]\ncodex = \"Assistant <assistant@example.test>\"\n", "worktrees = true\n", "global: Add probe Assistant <assistant@example.test>", false},
		{"project", "[commit]\nmessage = \"global: {title}\"\n[commit.identities]\ncodex = \"Assistant <assistant@example.test>\"\n", "worktrees = false\ncommit_message = \"project: {title} {lane}\"\n", "project: Add probe Assistant <assistant@example.test>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, git := worktreeConfigFixture(t)
			writeWorktreeConfig(t, config.UserFile(), tc.user)
			writeWorktreeConfig(t, config.ProjectFile(root), tc.project)
			env, err := (&app{}).loadEnv(false, false)
			if err != nil {
				t.Fatal(err)
			}
			if env.Roster.Worktrees() != tc.on {
				t.Fatalf("Worktrees = %v, want %v", env.Roster.Worktrees(), tc.on)
			}
			if env.Project.CommitMessage != nil && env.Roster.CommitTemplate() != *env.Project.CommitMessage {
				t.Fatalf("project template lost: %q", env.Roster.CommitTemplate())
			}
			writeWorktreeConfig(t, filepath.Join(root, "base.txt"), "changed\n")
			task := &dispatch.Task{Project: root, Title: "Add probe", Run: "r-test", Task: "1",
				Route: &router.Route{FamilyID: "codex-sol", Model: "gpt-6.1-sol"}}
			rec := &dispatch.Record{Path: t.TempDir(), Outcome: dispatch.Outcome{Outcome: dispatch.OutcomeVerified},
				Verification: dispatch.Verification{Changed: []string{"base.txt"}}}
			if err := dispatch.CommitChanges(env, task, rec); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(git("log", "-1", "--format=%B")); got != tc.message {
				t.Fatalf("message = %q, want %q", got, tc.message)
			}
		})
	}
}

func TestDoctorWorktrees(t *testing.T) {
	root, _ := worktreeConfigFixture(t)
	if err := os.Mkdir(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWorktreeConfig(t, config.ProjectFile(root), "checks = [\"go test ./...\"]\n")
	for _, setting := range []string{"on", "project", "global"} {
		if setting == "project" {
			writeWorktreeConfig(t, config.ProjectFile(root), "worktrees = false\n")
			writeWorktreeConfig(t, filepath.Join(root, "base.txt"), "dirty\n")
		}
		if setting == "global" {
			writeWorktreeConfig(t, config.UserFile(), "[run]\nworktrees = false\n")
			writeWorktreeConfig(t, config.ProjectFile(root), "worktrees = true\n")
		}
		output, err := doctorOutput(t)
		if err != nil {
			t.Fatal(err)
		}
		output = strings.Join(strings.Fields(output), " ")
		want := "worktrees on"
		if setting == "project" {
			want = "worktrees off (" + config.ProjectFile(root) + ")"
		} else if setting == "global" {
			want = "worktrees off (" + config.UserFile() + ")"
		}
		if !strings.Contains(output, want) || !strings.Contains(output, "dependency links vendor") {
			t.Fatalf("doctor lost mode or links: %s", output)
		}
		warning := "Runs use the tree one Task at a time until you commit or stash"
		if strings.Contains(output, warning) != (setting != "on") {
			t.Fatalf("doctor warning: %s", output)
		}
	}
}
