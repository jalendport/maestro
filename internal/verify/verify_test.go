package verify

import (
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"internal/probe/**", "internal/probe/probe.go", true},
		{"internal/probe/**", "internal/probe/sub/x.go", true},
		{"internal/probe/**", "internal/cli/cli.go", false},
		{"internal/probe", "internal/probe/probe.go", true},
		{"*.md", "README.md", true},
		{"*.md", "docs/README.md", true},
		{"**/*_test.go", "a/b/c_test.go", true},
		{"**/*_test.go", "c_test.go", true},
		{"cmd/probe.go", "cmd/probe.go", true},
		{"internal/**.go", "internal/probe.go", true},
		{"internal/**.go", "internal/probe/probe.go", false},
		{"internal/p**e.go", "internal/probe.go", true},
		{"internal/p**e.go", "internal/probe/probe.go", false},
		{"internal/**/probe.go", "internal/probe.go", true},
		{"internal/**/probe.go", "internal/a/b/probe.go", true},
		{"internal/probe/**", "internal/probe", false},
		{"README.md", "docs/README.md", true},
		{"/README.md", "README.md", true},
		{"/README.md", "docs/README.md", false},
		{"/*.md", "docs/README.md", false},
		{"internal/*", "internal/probe/probe.go", true},
		{"internal/pro*", "internal/probe/probe.go", true},
		{"**/probe", "internal/probe/probe.go", true},
		{"internal/pro*/", "internal/probe/probe.go", true},
		{"internal/pro*/", "internal/probe", false},
		{"probe/", "internal/probe/probe.go", true},
		{"internal/*", "other/internal/probe/probe.go", false},
		{"*.[ch]", "x.c", true},
		{"*.[ch]", "docs/x.h", true},
		{"*.[ch]", "x.go", false},
		{"*.[a-c]", "x.b", true},
		{"*.[!h]", "x.c", true},
		{"*.[!h]", "x.h", false},
		{`file\*.go`, "file*.go", true},
		{`file\*.go`, "file1.go", false},
		{`\!README.md`, "!README.md", true},
		{`\[ch\].go`, "[ch].go", true},
		{"!internal/probe/**", "internal/probe/probe.go", true},
		{"README.md", "docs/readme.md", false},
		{"internal/?.go", "internal/x.go", true},
		{"internal/?.go", "internal/xy.go", false},
		{"", "README.md", false},
	}
	for _, c := range cases {
		if got := Match(c.glob, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.glob, c.path, got)
		}
	}
}

func TestScopeViolations(t *testing.T) {
	v := ScopeViolations([]string{"a/x.go", "b/y.go", ".agents/maestro/runs/r/t/result.md"}, []string{"a/**"}, ".agents/maestro/runs")
	if len(v) != 1 || v[0] != "b/y.go" {
		t.Fatalf("%v", v)
	}
	t.Run("last matching pattern decides", func(t *testing.T) {
		changed := []string{"internal/main.go", "internal/probe/probe.go", "internal/probe/keep.go", "other.go"}
		touch := []string{"internal/**", "!internal/probe/**", "internal/probe/keep.go"}
		want := []string{"internal/probe/probe.go", "other.go"}
		if got := ScopeViolations(changed, touch, ""); !reflect.DeepEqual(got, want) {
			t.Fatalf("ScopeViolations() = %v, want %v", got, want)
		}
	})
}
