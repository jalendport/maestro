package brief

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const plan = `---
access: edit
commit: false
---
# Probe feature

## Add the probe command
- category: implement
- level: some
- touch: [internal/probe/**]

### Goal
Add it.

### Done
- It prints ok.

## Document the probe command
- category: prose
- access: edit
- blocked_by: [1]

### Goal
Write the doc.

### Done
- README has a section.

## Review the probe command
- category: review
- blocked_by: ["Add the probe command"]

### Goal
Review.

### Done
- Findings listed.
`

func TestParsePlan(t *testing.T) {
	p, err := ParsePlanText(plan)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Probe feature" || len(p.Tasks) != 3 {
		t.Fatalf("%+v", p)
	}
	a, d, r := p.Tasks[0], p.Tasks[1], p.Tasks[2]
	if a.Access != "edit" || a.Category != "implement" || a.Touch[0] != "internal/probe/**" || len(a.BlockedBy) != 0 {
		t.Fatalf("task 1: %+v", a.Fields)
	}
	if d.Access != "edit" || d.BlockedBy[0] != "Add the probe command" {
		t.Fatalf("task 2: %+v", d.Fields)
	}
	if r.Access != "edit" || r.EffectiveAccess() != "edit" || r.BlockedBy[0] != "Add the probe command" {
		t.Fatalf("task 3: %+v", r.Fields)
	}
	if d.Goal != "Write the doc." || d.Done != "- README has a section." {
		t.Fatalf("task 2 text: %q %q", d.Goal, d.Done)
	}
}

func TestPlanContext(t *testing.T) {
	const introduction = "All Tasks use the same data.\n\n### Shared facts\n- The data is read-only."
	const tasks = `
## First Task
### Goal
Read the data.
### Done
The data is read.
### Context
Read the first row.

## Second Task
### Goal
Read the data.
### Done
The data is read.
`
	for _, tt := range []struct {
		name         string
		prefix       string
		introduction string
	}{
		{"title and introduction", "# Plan\n\n" + introduction + "\n", introduction},
		{"front matter and introduction", "---\naccess: read\n---\n\n" + introduction + "\n", introduction},
		{"front matter and title and introduction", "---\naccess: read\n---\n# Plan\n\n" + introduction + "\n", introduction},
		{"introduction only", introduction + "\n", introduction},
		{"title only", "# Plan\n\n", ""},
		{"front matter only", "---\naccess: read\n---\n", ""},
		{"front matter and title only", "---\naccess: read\n---\n# Plan\n\n \t\n", ""},
		{"no introduction", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParsePlanText(tt.prefix + tasks)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.Tasks) != 2 {
				t.Fatalf("Tasks = %d, want 2", len(p.Tasks))
			}
			for i, task := range p.Tasks {
				want := ""
				if tt.introduction != "" {
					want = "Shared context of the Plan:\n\n" + tt.introduction
				}
				if i == 0 {
					if want != "" {
						want += "\n\n"
					}
					want += "Read the first row."
				}
				if task.Context != want {
					t.Fatalf("Task %d Context = %q, want %q", i+1, task.Context, want)
				}
				path := filepath.Join(t.TempDir(), "brief.md")
				if err := Write(task, path); err != nil {
					t.Fatal(err)
				}
				b, err := Parse(path)
				if err != nil {
					t.Fatal(err)
				}
				rendered := Render(b, RenderOpts{Access: AccessRead})
				sections := SplitSections(rendered)
				if got := sections["Context"]; got != want {
					t.Errorf("Task %d rendered Context = %q, want %q", i+1, got, want)
				}
				if _, ok := sections["Context"]; ok && want == "" {
					t.Errorf("Task %d has an empty Context section", i+1)
				}
			}
		})
	}
}

func TestPlanRejectsUnknownBlocker(t *testing.T) {
	if _, err := ParsePlanText("## A\n- blocked_by: [9]\n### Goal\ng\n### Done\nd\n"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestPlanRejectsDependencyCycles(t *testing.T) {
	for _, tt := range []struct {
		name         string
		dependencies []string
		want         string
	}{
		{name: "self by number", dependencies: []string{"1"}, want: `"A" -> "A"`},
		{name: "self by title", dependencies: []string{"A"}, want: `"A" -> "A"`},
		{name: "two tasks", dependencies: []string{"2", "A"}, want: `"A" -> "B" -> "A"`},
		{name: "three tasks", dependencies: []string{"2", "3", "1"}, want: `"A" -> "B" -> "C" -> "A"`},
		{name: "disconnected cycle", dependencies: []string{"", "3", "2"}, want: `"B" -> "C" -> "B"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var src strings.Builder
			for i, dependency := range tt.dependencies {
				fmt.Fprintf(&src, "## %c\n- blocked_by: [%s]\n### Goal\ng\n### Done\nd\n", 'A'+i, dependency)
			}
			_, err := ParsePlanText(src.String())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want cycle %s", err, tt.want)
			}
		})
	}
}

func TestPlanAcceptsSharedDependencies(t *testing.T) {
	var src strings.Builder
	for i, dependencies := range []string{"2, 3", "4", "4", ""} {
		fmt.Fprintf(&src, "## %c\n- blocked_by: [%s]\n### Goal\ng\n### Done\nd\n", 'A'+i, dependencies)
	}
	if _, err := ParsePlanText(src.String()); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRoundTrip(t *testing.T) {
	p, _ := ParsePlanText(plan)
	path := filepath.Join(t.TempDir(), "b.md")
	if err := Write(p.Tasks[0], path); err != nil {
		t.Fatal(err)
	}
	b, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "Add the probe command" || b.Category != "implement" || b.Touch[0] != "internal/probe/**" || b.Goal != "Add it." {
		t.Fatalf("%+v %q", b.Fields, b.Goal)
	}
}

func TestPlanTaskNamesTodo(t *testing.T) {
	p, err := ParsePlanText("# Plan\n## Task\n- todo: 123\n### Goal\nWork\n### Done\nDone\n")
	if err != nil || p.Tasks[0].Todo != 123 {
		t.Fatalf("Plan=%+v err=%v", p, err)
	}
}

func TestPlanTabIsPerTask(t *testing.T) {
	src := "---\ntab: Shared\n---\n# P\n\n## First task\n- tab: First\n\n### Goal\nx\n### Done\ny\n\n## Second task\n\n### Goal\nx\n### Done\ny\n"
	p, err := ParsePlanText(src)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Tasks[0].Tab; got != "First" {
		t.Errorf("task 1 tab = %q", got)
	}
	if got := p.Tasks[1].Tab; got != "" {
		t.Errorf("task 2 tab = %q, want none inherited", got)
	}
}
