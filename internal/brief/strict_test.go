package brief

import (
	"strings"
	"testing"
)

func TestBriefRejectsUnknownFrontMatterField(t *testing.T) {
	_, err := ParseText("---\ntitle: Test\ncheck: [true]\n---\n## Goal\ng\n## Done\nd\n")
	if err == nil || !strings.Contains(err.Error(), "field check not found") {
		t.Fatalf("error = %v, want unknown check field", err)
	}
}

func TestPlanRejectsUnknownFields(t *testing.T) {
	for _, src := range []string{
		"---\ncheck: [true]\n---\n## Test\n### Goal\ng\n### Done\nd\n",
		"## Test\n- check: [true]\n### Goal\ng\n### Done\nd\n",
	} {
		_, err := ParsePlanText(src)
		if err == nil || !strings.Contains(err.Error(), "field check not found") {
			t.Fatalf("error = %v, want unknown check field", err)
		}
	}
}

func TestRenderedRulesMatchReportShape(t *testing.T) {
	for _, council := range []bool{false, true} {
		text := Render(&Brief{Fields: Fields{Title: "Test"}}, RenderOpts{Access: AccessRead, Council: council})
		rules := strings.Split(strings.Split(text, "## Rules\n")[1], "## Report\n")[0]
		if strings.Contains(rules, "## Blocked") || !strings.Contains(rules, "## Concerns") {
			t.Fatalf("rules contradict read shape: %s", rules)
		}
		if council && strings.Contains(rules, "STATUS:") {
			t.Fatalf("Council rules require STATUS: %s", rules)
		}
	}
}
