package brief

import (
	"strings"
	"testing"
)

func TestRenderTouchIncludesTestFiles(t *testing.T) {
	b, err := ParseText(sample)
	if err != nil {
		t.Fatal(err)
	}
	out := Render(b, RenderOpts{Access: AccessEdit})
	want := "- Change nothing outside these paths: `internal/probe/**`; test files are always in scope.\n"
	if !strings.Contains(out, want) {
		t.Fatalf("Rules line missing: %s", out)
	}
}

func TestRenderDefinesBlockedAndConcerns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		access  string
		council bool
		section string
		recover string
	}{
		{"edit", AccessEdit, false, "Blocked", "Deviations"},
		{"read", AccessRead, false, "Concerns", "Findings"},
		{"Council", AccessRead, true, "Concerns", "Findings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Render(&Brief{}, RenderOpts{Access: tc.access, Council: tc.council})
			for _, want := range []string{
				"In `## " + tc.section + "`, list only a command that the Harness denied",
				"a Done item that you could not make true and why",
				"A failed command that you replaced with one that worked is not",
				"put it under `## " + tc.recover + "` when it matters",
				"Write `none` when",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("Brief lacks %q:\n%s", want, out)
				}
			}
			if strings.Contains(out, "blocked or denied command") || strings.Contains(out, "denied or impossible commands") {
				t.Errorf("Brief contains the old Blocked definition:\n%s", out)
			}
		})
	}
}
