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

func TestContextGlossaryFixture(t *testing.T) {
	b, err := Parse("testdata/context-glossary.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.Goal, "```md\n## {Group}\n\n### {Term}\n{Definition}\n```") || !strings.HasSuffix(b.Goal, "Keep the example in the Goal.") {
		t.Fatalf("Goal lost fenced text: %q", b.Goal)
	}
}

func TestParsersKeepFencedHeadings(t *testing.T) {
	for _, fence := range []struct{ name, open, middle, close string }{
		{"backticks", "```md", "", "```"},
		{"tildes", "~~~md", "", "~~~"},
		{"long fence", "````md", "```\n## Still fenced\n", "`````"},
		{"different marker", "~~~md", "```\n## Still fenced\n", "~~~"},
		{"indented fence", "   ```md", "", "   ```  "},
		{"closing info", "```md", "```text\n## Still fenced\n", "```"},
	} {
		t.Run(fence.name, func(t *testing.T) {
			example := fence.open + "\n## Example\n### Checks\n" + fence.middle + fence.close
			goal := "Before.\n" + example + "\nAfter."
			b, err := ParseText("# Brief\n## Goal\n" + goal + "\n## Done\nDone.\n")
			if err != nil {
				t.Fatal(err)
			}
			p, err := ParsePlanText("# Plan\n## Task\n### Goal\n" + goal + "\n### Done\nDone.\n## Next\n### Goal\nNext.\n### Done\nDone.\n")
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Tasks) != 2 || p.Tasks[0].Goal != goal || b.Goal != goal || b.Done != "Done." || p.Tasks[0].Done != "Done." {
				t.Fatalf("sections changed: Brief %+v, Plan %+v", b, p)
			}
			r := ParseReport("## Summary\n" + goal + "\n## Blocked\nnone\nSTATUS: done\n")
			if r.Status != StatusDone || r.Sections["Summary"] != goal || strings.Join(r.Order, ",") != "Summary,Blocked" || len(r.Blocked) != 0 {
				t.Fatalf("Report changed: %+v", r)
			}
		})
	}
}

func TestUnknownHeadingErrorsFollowFileOrder(t *testing.T) {
	for range 50 {
		_, err := ParseText("# Brief\n## Goal\ng\n## Done\nd\n## Zebra\nfirst\n## Alpha\nsecond\n")
		want := `unknown heading "## Zebra" (one of: Goal, Done, Context, Files)`
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %s", err, want)
		}
		_, err = ParsePlanText("# Plan\n## Task\n### Goal\ng\n### Done\nd\n### Zebra\nfirst\n### Alpha\nsecond\n")
		want = `task "Task": unknown heading "### Zebra" (one of: Goal, Done, Context, Files)`
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %s", err, want)
		}
	}
}

func TestUnclosedFenceKeepsRemainingText(t *testing.T) {
	text := "# Brief\n## Goal\ng\n## Done\nDone.\n~~~md\n## Checks\n### Unknown\n"
	b, err := ParseText(text)
	if err != nil {
		t.Fatal(err)
	}
	if b.Done != "Done.\n~~~md\n## Checks\n### Unknown" {
		t.Fatalf("Done = %q", b.Done)
	}
}
