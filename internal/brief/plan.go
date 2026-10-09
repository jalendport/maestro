package brief

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Plan is one parsed Plan file: Run-wide defaults plus one Task per
// "## <title>" section (task-brief.md section 13).
type Plan struct {
	Title    string
	Defaults Fields
	Tasks    []*Brief
	Path     string
}

var taskHeadingRe = regexp.MustCompile(`(?m)^## +(.+?)[ \t]*$`)
var subHeadingRe = regexp.MustCompile(`(?m)^### +(.+?)[ \t]*$`)

// ParsePlan reads a Plan file.
func ParsePlan(path string) (*Plan, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := ParsePlanText(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	abs, _ := filepath.Abs(path)
	p.Path = abs
	if p.Title == "" {
		p.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return p, nil
}

// ParsePlanText parses Plan file text. A Task section holds an optional
// "- key: value" list of fields, then "### Goal", "### Done", "### Context",
// "### Files". blocked_by names Tasks by plan number (1-based) or title.
func ParsePlanText(src string) (*Plan, error) {
	front, body := SplitFrontMatter(src)
	p := &Plan{}
	if front != "" {
		if err := yaml.Unmarshal([]byte(front), &p.Defaults); err != nil {
			return nil, fmt.Errorf("front matter: %w", err)
		}
	}
	p.Title = firstH1(body)
	locs := taskHeadingRe.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		return nil, fmt.Errorf("plan has no \"## <task>\" sections")
	}
	for i, loc := range locs {
		title := body[loc[2]:loc[3]]
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		section := body[loc[1]:end]
		b, err := parseTaskSection(title, section, p.Defaults)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", title, err)
		}
		p.Tasks = append(p.Tasks, b)
	}
	// Resolve blocked_by to titles so every reference is one shape.
	byNum := map[string]string{}
	byTitle := map[string]bool{}
	for i, t := range p.Tasks {
		byNum[strconv.Itoa(i+1)] = t.Title
		byTitle[t.Title] = true
	}
	for _, t := range p.Tasks {
		for i, ref := range t.BlockedBy {
			ref = strings.TrimSpace(ref)
			if title, ok := byNum[ref]; ok {
				t.BlockedBy[i] = title
				continue
			}
			if !byTitle[ref] {
				return nil, fmt.Errorf("task %q: blocked_by %q names no task", t.Title, ref)
			}
			t.BlockedBy[i] = ref
		}
	}
	return p, nil
}

func parseTaskSection(title, section string, defaults Fields) (*Brief, error) {
	b := &Brief{Sections: map[string]string{}}
	b.Fields = defaults
	b.Fields.Title = title
	b.Fields.BlockedBy = nil
	// Field list: leading "- key: value" lines before the first subheading.
	head := section
	if m := subHeadingRe.FindStringIndex(section); m != nil {
		head = section[:m[0]]
	}
	var fieldLines []string
	for _, line := range strings.Split(head, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") && strings.Contains(t, ":") {
			fieldLines = append(fieldLines, strings.TrimPrefix(t, "- "))
		}
	}
	if len(fieldLines) > 0 {
		var f Fields
		if err := yaml.Unmarshal([]byte(strings.Join(fieldLines, "\n")), &f); err != nil {
			return nil, fmt.Errorf("fields: %w", err)
		}
		b.Fields = mergeFields(defaults, f)
		b.Fields.Title = title
	}
	// Subsections.
	subs := subHeadingRe.FindAllStringSubmatchIndex(section, -1)
	for i, loc := range subs {
		name := section[loc[2]:loc[3]]
		end := len(section)
		if i+1 < len(subs) {
			end = subs[i+1][0]
		}
		text := strings.TrimSpace(section[loc[1]:end])
		b.Sections[name] = text
		switch strings.ToLower(name) {
		case "goal":
			b.Goal = text
		case "done":
			b.Done = text
		case "context":
			b.Context = text
		case "files":
			b.Files = listItems(text)
		}
	}
	return b, b.Validate()
}

// mergeFields overlays the set fields of t on d.
func mergeFields(d, t Fields) Fields {
	out := d
	if t.Category != "" {
		out.Category = t.Category
	}
	if t.Level != "" {
		out.Level = t.Level
	}
	if t.Access != "" {
		out.Access = t.Access
	}
	if t.Dirs != nil {
		out.Dirs = append(append([]string{}, d.Dirs...), t.Dirs...)
	}
	if t.Touch != nil {
		out.Touch = t.Touch
	}
	if t.Checks != nil {
		out.Checks = append(append([]string{}, d.Checks...), t.Checks...)
	}
	if t.Commit != nil {
		out.Commit = t.Commit
	}
	if t.Lane != "" {
		out.Lane = t.Lane
	}
	if t.Floor != "" {
		out.Floor = t.Floor
	}
	if t.Ceiling != "" {
		out.Ceiling = t.Ceiling
	}
	if t.WallLimit != "" {
		out.WallLimit = t.WallLimit
	}
	if t.Facts != nil {
		out.Facts = t.Facts
	}
	out.BlockedBy = t.BlockedBy
	return out
}

// Write serializes a Brief back to the single-file format, so a Plan Task
// can be dispatched through the same path as a Brief file.
func Write(b *Brief, path string) error {
	fields := b.Fields
	fields.BlockedBy = nil
	fm, err := yaml.Marshal(fields)
	if err != nil {
		return err
	}
	var w strings.Builder
	w.WriteString("---\n")
	w.Write(fm)
	w.WriteString("---\n\n")
	fmt.Fprintf(&w, "## Goal\n%s\n\n## Done\n%s\n", strings.TrimSpace(b.Goal), strings.TrimSpace(b.Done))
	if strings.TrimSpace(b.Context) != "" {
		fmt.Fprintf(&w, "\n## Context\n%s\n", strings.TrimSpace(b.Context))
	}
	if len(b.Files) > 0 {
		w.WriteString("\n## Files\n")
		for _, f := range b.Files {
			fmt.Fprintf(&w, "- %s\n", f)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(w.String()), 0o644)
}
