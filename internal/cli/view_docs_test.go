package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSoloAndEnforcementDocumentation(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	skill := read("skills/maestro/SKILL.md")
	rules := strings.SplitN(strings.SplitN(skill, "rules the formats cannot enforce:\n", 2)[1], "\n## ", 2)[0]
	if !strings.Contains(skill, "Four rules") || strings.Count(rules, "\n- ") != 4 || strings.Contains(rules, "`tab:`") {
		t.Fatalf("Brief rules are incomplete: %s", rules)
	}
	configuration := read("docs/configuration.md")
	if !strings.Contains(configuration, "cannot detect three limits") || !strings.Contains(configuration, "Grok Lane through the `transcriptPath` key") {
		t.Fatal("docs/configuration.md does not state the Grok transcript limit")
	}
	changelog := strings.SplitN(read("CHANGELOG.md"), "## 1.2.0", 2)[0]
	if !strings.Contains(changelog, "read-only Solo MCP tools for Claude Lanes with a Solo link") || !strings.Contains(changelog, "other Solo tools are denied without a Relay") {
		t.Fatal("the one-shot changelog omits the Claude Solo tool set or its limits")
	}
}
