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

func TestBriefRejectsUnknownHeadings(t *testing.T) {
	for _, name := range []string{"Context (correction from the Orchestrator)", "Checks", "Rules", "Report", "Gola"} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseText("# Test\n## Goal\ng\n## Done\nd\n## " + name + "\ntext\n")
			want := `unknown heading "## ` + name + `" (one of: Goal, Done, Context, Files)`
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %s", err, want)
			}
		})
	}
}

func TestPlanRejectsUnknownTaskHeadings(t *testing.T) {
	_, err := ParsePlanText("# Plan\n## Test\n### Goal\ng\n### Done\nd\n### Checks\ntext\n")
	want := `task "Test": unknown heading "### Checks" (one of: Goal, Done, Context, Files)`
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

func TestBriefAndPlanAcceptMixedCaseHeadings(t *testing.T) {
	sections := "## gOaL\ng\n## DONE\nd\n## context\nc\n## FiLeS\n- file.go\n"
	b, err := ParseText("# Brief title\n" + sections)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePlanText("# Plan title\n## Task title\n" + strings.ReplaceAll(sections, "## ", "### "))
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Brief title" || p.Title != "Plan title" || len(p.Tasks) != 1 || p.Tasks[0].Title != "Task title" {
		t.Fatalf("titles changed: Brief %+v, Plan %+v", b, p)
	}
	for _, parsed := range []*Brief{b, p.Tasks[0]} {
		if parsed.Goal != "g" || parsed.Done != "d" || parsed.Context != "c" || len(parsed.Files) != 1 || parsed.Files[0] != "file.go" {
			t.Fatalf("sections changed: %+v", parsed)
		}
	}
}

func TestReportAcceptsExtraHeading(t *testing.T) {
	r := ParseReport("## Summary\nok\n## Extra heading\nKeep this text.\nSTATUS: done\n")
	if r.Status != StatusDone || r.Sections["Summary"] != "ok" || r.Sections["Extra heading"] != "Keep this text." || len(r.Order) != 2 || r.Order[1] != "Extra heading" {
		t.Fatalf("Report changed: %+v", r)
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
