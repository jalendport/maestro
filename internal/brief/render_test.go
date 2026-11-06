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
