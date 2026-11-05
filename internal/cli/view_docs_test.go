package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestViewAndEnforcementDocumentation(t *testing.T) {
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
	if !strings.Contains(skill, "Five rules") || strings.Count(rules, "\n- ") != 5 || !strings.Contains(rules, "`tab:`") || !strings.Contains(rules, "Solo View tab") {
		t.Fatalf("Brief rules are incomplete: %s", rules)
	}
	for _, path := range []string{"README.md", "CONTEXT.md"} {
		text := read(path)
		if !strings.Contains(text, "cannot detect three limits") || !strings.Contains(text, "Grok Lane through the `transcriptPath` key") {
			t.Fatalf("%s does not state the Grok transcript limit", path)
		}
	}
	changelog := strings.SplitN(read("CHANGELOG.md"), "## 1.2.0", 2)[0]
	if !strings.Contains(changelog, "Claude Lanes with a Solo link get a read-only Solo MCP set") || !strings.Contains(changelog, "all `SOLO_` variables stay removed") || !strings.Contains(changelog, "Other Solo tools are denied without a Relay") {
		t.Fatal("the one-shot changelog omits the Claude Solo tool set or its limits")
	}
}
