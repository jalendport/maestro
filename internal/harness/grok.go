package harness

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Grok is the Grok CLI adapter. Day one: catalog and version only; its
// launch shapes land with the Solo-tab milestone.
type Grok struct{ bin string }

func (g *Grok) Name() string { return "grok" }
func (g *Grok) Bin() string  { return g.bin }

func (g *Grok) Version() (string, error) { return versionOf(g.bin) }

var grokModelRe = regexp.MustCompile(`(?m)^\s+[*-]\s+(\S+)`)

// Catalog parses the plain text of `grok models`.
func (g *Grok) Catalog() ([]Model, string, error) {
	if g.bin == "" {
		return nil, "", fmt.Errorf("not installed")
	}
	out, err := exec.Command(g.bin, "models").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, "", err
	}
	var models []Model
	for _, m := range grokModelRe.FindAllStringSubmatch(string(out), -1) {
		models = append(models, Model{ID: m[1], Efforts: []string{"low", "medium", "high", "xhigh"}})
	}
	hint := ""
	if m := regexp.MustCompile(`logged in with (\S+)`).FindStringSubmatch(string(out)); m != nil {
		hint = strings.TrimSuffix(m[1], ".")
	}
	return models, hint, nil
}

func (g *Grok) Enforcement(access string) string {
	switch access {
	case "read":
		return BestEffort
	case "edit":
		return BestEffort
	}
	return NoShape
}

func (g *Grok) Build(l Launch) (*Command, error) { return nil, ErrNotReady }

func (g *Grok) NewParser() Parser { return &textParser{out: &Outcome{}} }
