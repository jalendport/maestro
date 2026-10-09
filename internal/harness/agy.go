package harness

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Agy is the Antigravity CLI adapter. Day one: catalog and version only.
type Agy struct{ bin string }

func (a *Agy) Name() string { return "agy" }
func (a *Agy) Bin() string  { return a.bin }

func (a *Agy) Version() (string, error) { return versionOf(a.bin) }

var agyModelRe = regexp.MustCompile(`^(gemini-[\w.-]+?)-(low|medium|high)\t`)

// Catalog parses the TSV of `agy models`. Effort-named rows collapse into one
// model with an effort list, which matches the family stanza shape.
func (a *Agy) Catalog() ([]Model, string, error) {
	if a.bin == "" {
		return nil, "", fmt.Errorf("not installed")
	}
	out, err := exec.Command(a.bin, "models").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, "", err
	}
	byID := map[string]*Model{}
	var order []string
	for _, line := range strings.Split(string(out), "\n") {
		m := agyModelRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if byID[m[1]] == nil {
			byID[m[1]] = &Model{ID: m[1]}
			order = append(order, m[1])
		}
		byID[m[1]].Efforts = append(byID[m[1]].Efforts, m[2])
	}
	var models []Model
	for _, id := range order {
		models = append(models, *byID[id])
	}
	return models, "GCP", nil
}

func (a *Agy) Enforcement(access string) string {
	if access == "read" {
		return Enforced
	}
	return NoShape
}

func (a *Agy) Build(l Launch) (*Command, error) { return nil, ErrNotReady }

func (a *Agy) NewParser() Parser { return &textParser{out: &Outcome{}} }

// textParser keeps the raw text for adapters without a JSON stream yet.
type textParser struct {
	out *Outcome
	buf strings.Builder
}

func (p *textParser) Outcome() *Outcome { p.out.Text = p.buf.String(); return p.out }

func (p *textParser) Line(line []byte) bool {
	p.buf.Write(line)
	p.buf.WriteByte('\n')
	return len(strings.TrimSpace(string(line))) > 0
}

var _ = fmt.Sprintf
