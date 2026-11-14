package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/run"
)

func TestCommandsRejectUnknownBriefHeadings(t *testing.T) {
	for _, verb := range []string{"dispatch", "run", "add", "resume"} {
		t.Run(verb, func(t *testing.T) {
			_, s := waitFixture(t)
			s.Tasks[0].State = run.TaskBlocked
			if err := s.Save(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.Project, "corrected.md")
			src := "# Test\n## Goal\ng\n## Done\nd\n## Context (correction from the Orchestrator)\nKeep this correction.\n"
			args := []string{verb, path}
			prefix := "## "
			if verb == "run" {
				src = "# Plan\n## Test\n" + strings.ReplaceAll(strings.TrimPrefix(src, "# Test\n"), "## ", "### ")
				prefix = "### "
			}
			if verb == "resume" {
				args = []string{verb, s.Run, "--task", "1", "--brief", path}
			}
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			output, err := waitCommand(t, args...)
			want := `unknown heading "` + prefix + `Context (correction from the Orchestrator)" (one of: Goal, Done, Context, Files)`
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("%s: %s, %v; want %s", verb, output, err, want)
			}
			if run.PendingResume(s.Folder, "") {
				t.Fatal("refused Brief queued a resume answer")
			}
			markers, err := filepath.Glob(filepath.Join(s.Folder, "control", "add-*.md"))
			if err != nil || len(markers) != 0 {
				t.Fatalf("refused Brief queued an added Task: %v, %v", markers, err)
			}
		})
	}
}
