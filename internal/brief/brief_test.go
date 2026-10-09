package brief

import (
	"strings"
	"testing"
)

const sample = `---
title: Add the probe command
category: implement
level: some
touch: [internal/probe/**]
checks: ["go test ./internal/probe/..."]
facts: {input: small}
---

## Goal
Add a probe verb.

## Done
- ` + "`maestro probe`" + ` prints ok.

## Files
- internal/cli/cli.go
`

func TestSplitFrontMatter(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		front string
		body  string
	}{
		{
			name: "no front matter",
			src:  "# Brief\n\n## Goal\nAdd a probe verb.\n",
			body: "# Brief\n\n## Goal\nAdd a probe verb.\n",
		},
		{
			name: "unclosed front matter",
			src:  "---\ntitle: Add the probe command\n\n## Goal\nAdd a probe verb.\n",
			body: "---\ntitle: Add the probe command\n\n## Goal\nAdd a probe verb.\n",
		},
		{
			name:  "CRLF line endings",
			src:   "---\r\ntitle: Add the probe command\r\n---\r\n\r\n## Goal\r\nAdd a probe verb.\r\n",
			front: "title: Add the probe command\r\n",
			body:  "\n\r\n## Goal\r\nAdd a probe verb.\r\n",
		},
		{
			name:  "UTF-8 BOM",
			src:   "\ufeff---\ntitle: Add the probe command\n---\n\n## Goal\nAdd a probe verb.\n",
			front: "title: Add the probe command\n",
			body:  "\n\n## Goal\nAdd a probe verb.\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			front, body := SplitFrontMatter(tc.src)
			if front != tc.front {
				t.Errorf("front = %q, want %q", front, tc.front)
			}
			if body != tc.body {
				t.Errorf("body = %q, want %q", body, tc.body)
			}
		})
	}
}

func TestParseBrief(t *testing.T) {
	b, err := ParseText(sample)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Add the probe command" || b.Category != "implement" || b.Level != "some" {
		t.Fatalf("fields: %+v", b.Fields)
	}
	if b.EffectiveAccess() != AccessEdit {
		t.Fatalf("access %q", b.EffectiveAccess())
	}
	if len(b.Files) != 1 || b.Files[0] != "internal/cli/cli.go" {
		t.Fatalf("files %v", b.Files)
	}
	if b.Touch[0] != "internal/probe/**" {
		t.Fatalf("touch %v", b.Touch)
	}
}

func TestParseBriefMissingDone(t *testing.T) {
	_, err := ParseText("---\ntitle: x\n---\n## Goal\ny\n")
	if err == nil || !strings.Contains(err.Error(), "## Done") {
		t.Fatalf("expected a missing Done error, got %v", err)
	}
}

func TestRenderHidesTierWords(t *testing.T) {
	b, _ := ParseText(sample)
	out := Render(b, RenderOpts{TaskID: "7f3a", RunID: "r-0001", Access: AccessEdit, ProjectChecks: []string{"go vet ./..."}, BriefChecks: b.Checks, ReportPath: "/x/result.md"})
	for _, banned := range []string{"some", "Daily", "effort", "cost"} {
		if strings.Contains(out, " "+banned+" ") {
			t.Fatalf("rendered brief leaks %q", banned)
		}
	}
	for _, want := range []string{"## Goal", "## Done", "Read the file `internal/cli/cli.go`", "`go vet ./...`", "`go test ./internal/probe/...`", "Do not commit.", "STATUS: done"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered brief lacks %q:\n%s", want, out)
		}
	}
}

func TestParseReport(t *testing.T) {
	r := ParseReport("## Summary\nok\n\n## Changed\n- a.go\n- b.go\n\n## Checks\n- `go test` — ok\n\n## Blocked\n- none\n\nSTATUS: partial\n")
	if r.Status != StatusPartial || len(r.Changed) != 2 || len(r.Blocked) != 0 || len(r.Checks) != 1 {
		t.Fatalf("%+v", r)
	}
	if ParseReport("no status here").Status != StatusUnshaped {
		t.Fatal("missing status must be unshaped")
	}
}
