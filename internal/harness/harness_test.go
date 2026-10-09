package harness

import (
	"os"
	"strings"
	"testing"
)

func feed(t *testing.T, p Parser, path string) *Outcome {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skip("no fixture", path)
	}
	for _, line := range strings.Split(string(b), "\n") {
		p.Line([]byte(line))
	}
	return p.Outcome()
}

func TestClaudeParserFixture(t *testing.T) {
	o := feed(t, (&Claude{}).NewParser(), "testdata/claude-stream.jsonl")
	if o.SessionID == "" || o.ModelRan == "" || o.Text != "OK" || o.IsError {
		t.Fatalf("%+v", o)
	}
	if len(o.Windows) != 2 || o.Tokens.Output == nil || *o.Tokens.Output != 4 {
		t.Fatalf("windows %v tokens %+v", o.Windows, o.Tokens)
	}
}

func TestCodexParserFixture(t *testing.T) {
	o := feed(t, (&Codex{}).NewParser(), "testdata/codex-stream.jsonl")
	if o.SessionID == "" || o.Text != "OK" || o.IsError || o.Tokens.Input == nil {
		t.Fatalf("%+v", o)
	}
}

func TestVersionLess(t *testing.T) {
	if !VersionLess("0.152.1", "0.156.1") || VersionLess("0.162.0", "0.156.1") || VersionLess("2.1.296", "2.1.296") {
		t.Fatal("version compare")
	}
}

func TestClassify(t *testing.T) {
	if classifyText("codex", "You've hit your usage limit").Class != "limit" {
		t.Fatal("limit")
	}
	if classifyText("codex", "something odd").Class != "transient" {
		t.Fatal("unknown must be transient")
	}
}
