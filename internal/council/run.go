package council

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/jev"
	"github.com/jalendport/maestro/internal/ledger"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/ui"
)

// Start runs every Round, merges, filters, and records the Council.
func Start(env *dispatch.Env, folder string) (*Council, error) {
	c, err := Load(folder)
	if err != nil {
		return nil, err
	}
	// Rehydrate families.
	for _, s := range c.Seats {
		f, ok := env.Roster.Family(s.Family)
		if !ok {
			return nil, fmt.Errorf("seat family %q is not in the roster", s.Family)
		}
		s.Family_ = f
	}
	c.Pid = os.Getpid()
	c.State = "running"
	_ = c.Save()
	ui.Eventf("council", "%s  %s  %d seats  %d rounds  → %s", c.Slug, titleCase(string(c.Tier)), len(c.Seats), c.RoundsAsked, relOrAbs(env.Root, c.Path))
	for round := 1; round <= c.RoundsAsked; round++ {
		c.RoundsRun = round
		runRound(env, c, round)
		_ = c.Save()
		if round < c.RoundsAsked && allConfirmed(c, round) {
			ui.Eventf("council", "%s  every seat confirmed after round %d; stopping early", c.Slug, round)
			break
		}
		if liveSeats(c, round) < 1 {
			break
		}
	}
	merge(env, c)
	c.State = "finished"
	c.Pid = 0
	c.EndedAt = time.Now()
	c.DurationS = c.EndedAt.Sub(c.StartedAt).Seconds()
	if err := c.Save(); err != nil {
		return nil, err
	}
	if err := ledger.Append(c); err != nil {
		ui.Warnf("ledger: %v", err)
	}
	answered := 0
	for _, s := range c.Seats {
		if last(s) != nil && last(s).Status == "ok" {
			answered++
		}
	}
	ui.Eventf("finished", "council %s  %d of %d seats answered  %d findings, %d filtered", c.Slug, answered, len(c.Seats), len(c.Findings), len(c.Filtered))
	return c, nil
}

// runRound dispatches every live Seat in parallel for one Round.
func runRound(env *dispatch.Env, c *Council, round int) {
	roundDir := c.Path
	if round > 1 {
		roundDir = filepath.Join(c.Path, fmt.Sprintf("round-%d", round))
	}
	_ = os.MkdirAll(roundDir, 0o755)
	// Challenge rounds: prior reports with the names removed.
	var priors []string
	if round > 1 {
		priorDir := filepath.Join(roundDir, "prior")
		_ = os.MkdirAll(priorDir, 0o755)
		n := 0
		for _, s := range c.Seats {
			r := seatRound(s, round-1)
			if r == nil || r.Status != "ok" || r.Report == "" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(c.Project, r.Report))
			if err != nil {
				continue
			}
			n++
			p := filepath.Join(priorDir, fmt.Sprintf("answer-%c.md", 'A'+n-1))
			_ = os.WriteFile(p, b, 0o644)
			rel, _ := filepath.Rel(c.Project, p)
			priors = append(priors, rel)
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(1, env.Roster.Concurrency.MaxParallel))
	for _, s := range c.Seats {
		if round > 1 {
			prev := seatRound(s, round-1)
			if prev == nil || prev.Status != "ok" {
				continue // a dropped Seat stays dropped
			}
		}
		wg.Add(1)
		go func(s *Seat) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			runSeat(env, c, s, round, roundDir, priors)
		}(s)
	}
	wg.Wait()
}

func runSeat(env *dispatch.Env, c *Council, s *Seat, round int, roundDir string, priors []string) {
	b := councilBrief(c, s, round, priors)
	sr := SeatRound{Round: round}
	seatDir := filepath.Join(roundDir, s.Family)
	_ = os.MkdirAll(seatDir, 0o755)
	if err := brief.Write(b, filepath.Join(seatDir, "brief.source.md")); err != nil {
		sr.Status = "failed"
		sr.Error = err.Error()
		s.Rounds = append(s.Rounds, sr)
		return
	}
	b.Path = filepath.Join(seatDir, "brief.source.md")
	req := router.Request{Seat: s.Family, SeatEffort: s.Effort, Access: brief.AccessRead, Level: c.Level, Project: env.Project, InSolo: dispatch.InSolo()}
	task, err := dispatch.PrepareIn(env, b, req, c.Slug, s.Family, roundDir)
	if err != nil {
		sr.Status = "failed"
		sr.Error = err.Error()
		s.Rounds = append(s.Rounds, sr)
		return
	}
	s.Route = task.Route
	task.MaxAttempt = 2
	rec, err := dispatch.Execute(env, task)
	if err != nil {
		sr.Status = "failed"
		sr.Error = err.Error()
		s.Rounds = append(s.Rounds, sr)
		return
	}
	sr.DurationS = rec.DurationS
	report, found := rec.Report()
	switch {
	case !found || rec.Outcome.Outcome == dispatch.OutcomeFailed || rec.Outcome.Outcome == dispatch.OutcomeTimedOut:
		sr.Status = "dropped"
		sr.Error = rec.Outcome.FailureClass + ": " + firstLine(rec.Outcome.ErrorText)
	default:
		sr.Status = "ok"
		sr.Position = report.Position
		if sr.Position == "" {
			sr.Position = "confirmed"
		}
		// The canonical per-Seat file: <family>.md beside the Seat folder.
		dst := filepath.Join(roundDir, s.Family+".md")
		_ = os.WriteFile(dst, []byte(report.Raw), 0o644)
		rel, _ := filepath.Rel(c.Project, dst)
		sr.Report = rel
	}
	s.Rounds = append(s.Rounds, sr)
}

// councilBrief builds the shared Brief for one Seat and Round.
func councilBrief(c *Council, s *Seat, round int, priors []string) *brief.Brief {
	var goal strings.Builder
	goal.WriteString(strings.TrimSpace(c.Question))
	goal.WriteString("\n\nFind what is wrong. Answer PASS only when nothing real is wrong.")
	if s.Author {
		goal.WriteString("\nA Lane of your family wrote this. Argue against it.")
	}
	if c.Context != "" {
		goal.WriteString("\n\n" + strings.TrimSpace(c.Context))
	}
	done := "- An answer under `## Answer`.\n- Every finding under `## Findings` names a file and line and a confidence word.\n- The last line is the position line."
	files := append([]string{}, c.Files...)
	if round > 1 {
		goal.WriteString("\n\nThis is a challenge round. Read the other answers from round " + fmt.Sprint(round-1) + " (names removed), then confirm, refine, or withdraw your own answer.")
		files = append(files, priors...)
	}
	b := &brief.Brief{Goal: goal.String(), Done: done, Files: files, Sections: map[string]string{}}
	b.Title = c.Question
	if len(b.Title) > 80 {
		b.Title = b.Title[:77] + "…"
	}
	b.Category = "second opinion"
	b.Access = brief.AccessRead
	return b
}

func seatRound(s *Seat, round int) *SeatRound {
	for i := range s.Rounds {
		if s.Rounds[i].Round == round {
			return &s.Rounds[i]
		}
	}
	return nil
}

func last(s *Seat) *SeatRound {
	if len(s.Rounds) == 0 {
		return nil
	}
	return &s.Rounds[len(s.Rounds)-1]
}

func allConfirmed(c *Council, round int) bool {
	live := 0
	for _, s := range c.Seats {
		r := seatRound(s, round)
		if r == nil || r.Status != "ok" {
			continue
		}
		live++
		if r.Position != "confirmed" {
			return false
		}
	}
	return live > 0
}

func liveSeats(c *Council, round int) int {
	n := 0
	for _, s := range c.Seats {
		if r := seatRound(s, round); r != nil && r.Status == "ok" {
			n++
		}
	}
	return n
}

// merge collects the findings of the last Round per Seat and asks Jev to
// mark each one; nothing is deleted.
func merge(env *dispatch.Env, c *Council) {
	type item struct {
		seat  string
		round int
		text  string
	}
	var items []item
	for _, s := range c.Seats {
		r := last(s)
		if r == nil || r.Status != "ok" || r.Report == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(c.Project, r.Report))
		if err != nil {
			continue
		}
		rep := brief.ParseReport(string(b))
		for _, line := range strings.Split(rep.Sections["Findings"], "\n") {
			t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
			if t == "" || strings.EqualFold(t, "none") {
				continue
			}
			items = append(items, item{s.Family, r.Round, t})
		}
	}
	answered := 0
	for _, s := range c.Seats {
		if r := last(s); r != nil && r.Status == "ok" {
			answered++
		}
	}
	c.SingleOpinion = answered <= 1
	if len(items) == 0 {
		return
	}
	probs := make([]float64, len(items))
	for i := range probs {
		probs[i] = 1
	}
	if env.Roster.Router.Backend == "rules" || (env.Project != nil && env.Project.Router == "rules") {
		c.FilterSkipped = "router = rules"
	} else if client, err := jev.New(env.Roster.Router.BaseURL, env.Roster.Router.Model, 5*time.Second); err != nil {
		c.FilterSkipped = err.Error()
	} else {
		questions := map[string]jev.Question{}
		state := map[string]any{"question": c.Question, "findings": map[string]string{}}
		for i, it := range items {
			key := fmt.Sprintf("f%d", i)
			state["findings"].(map[string]string)[key] = it.text
			questions[key] = jev.Noul(fmt.Sprintf("Is finding %s a real, specific problem with evidence (a file and line), not a restatement, a style note, or a guess?", key))
		}
		if env.Project != nil {
			client.Redact = env.Project.Redact
		}
		if resp, err := client.Ask(context.Background(), state, questions); err != nil {
			c.FilterSkipped = err.Error()
		} else {
			for i := range items {
				if a, ok := resp.Answers[fmt.Sprintf("f%d", i)]; ok {
					probs[i] = a.Noul
				}
			}
		}
	}
	for i, it := range items {
		f := Finding{Seat: it.seat, Round: it.round, Text: it.text, Probability: probs[i]}
		if probs[i] >= 0.50 {
			c.Findings = append(c.Findings, f)
		} else {
			c.Filtered = append(c.Filtered, f)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func relOrAbs(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}
