package verify

import (
	"reflect"
	"testing"
)

func TestInScopeTestFiles(t *testing.T) {
	touch := []string{"internal/hook/lane.go"}
	for _, path := range []string{
		"hook_test.go", "internal/cli/hook_test.go",
		"hook_test.py", "web/hook_test.ts",
		"hook.test.js", "web/hook.test.ts",
		"hook.spec.js", "web/hook.spec.ts", "web/tests/a.spec.ts",
		"HookTest.php", "php/HookTest.php",
		"test/fixture.txt", "web/test/fixture.txt",
		"tests/fixture.txt", "web/tests/fixture.txt",
		"__tests__/fixture.txt", "web/__tests__/fixture.txt",
		"spec/fixture.txt", "web/spec/fixture.txt",
		"testdata/fixture.txt", "internal/cli/testdata/fixture.txt",
	} {
		t.Run(path, func(t *testing.T) {
			if !InScope(path, touch) {
				t.Fatalf("test file %q is outside scope", path)
			}
			if !InScope(path, []string{"!**"}) {
				t.Fatalf("negation excluded test file %q", path)
			}
		})
	}
}

func TestInScopeTouchPatterns(t *testing.T) {
	for _, tc := range []struct {
		path  string
		touch []string
		want  bool
	}{
		{"internal/cli/run.go", nil, true},
		{"internal/cli/run.go", []string{}, true},
		{"internal/hook/lane.go", []string{"internal/hook/lane.go"}, true},
		{"internal/cli/run.go", []string{"internal/hook/lane.go"}, false},
		{"internal/cli/run.go", []string{"internal/**", "!internal/cli/**"}, false},
		{"internal/cli/run.go", []string{"internal/**", "!internal/cli/**", "internal/cli/run.go"}, true},
	} {
		if got := InScope(tc.path, tc.touch); got != tc.want {
			t.Errorf("InScope(%q, %q) = %v, want %v", tc.path, tc.touch, got, tc.want)
		}
	}
	for _, path := range []string{
		"internal/cli/testing.go", "web/test.ts", "web/tests.ts", "web/spec.ts",
		"web/testdata.ts", "web/__tests__.ts", "php/HookTests.php", "web/testing/a.ts",
	} {
		if InScope(path, []string{"internal/hook/lane.go"}) {
			t.Errorf("non-test file %q is inside scope", path)
		}
	}
}

func TestScopeViolationsTestFiles(t *testing.T) {
	changed := []string{
		"internal/hook/lane.go", "internal/cli/hook_test.go", "web/tests/a.spec.ts",
		"internal/cli/run.go", ".agents/maestro/runs/r/t/result.md",
	}
	want := []string{"internal/cli/run.go"}
	if got := ScopeViolations(changed, []string{"internal/hook/lane.go"}, ".agents/maestro/runs"); !reflect.DeepEqual(got, want) {
		t.Fatalf("ScopeViolations() = %v, want %v", got, want)
	}
	for _, touch := range [][]string{nil, {}} {
		if got := ScopeViolations(changed, touch, ""); got != nil {
			t.Fatalf("empty touch gives violations: %v", got)
		}
	}
}
