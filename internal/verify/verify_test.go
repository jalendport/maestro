package verify

import "testing"

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
		{"*.md", "docs/README.md", false},
		{"**/*_test.go", "a/b/c_test.go", true},
		{"**/*_test.go", "c_test.go", true},
		{"cmd/probe.go", "cmd/probe.go", true},
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
}
