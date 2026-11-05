package dispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/hook"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

const canaryBrief = `---
title: Check the Harness
category: triage
level: none
access: read
---
## Goal
The installed Harness can read a file and return a Report.

## Done
Read the file named in Files with the shell tool: run cat with that file's absolute path. Then return a short Report with a valid Status line. Do not start a sub-agent or change any project file.

## Files
- canary.txt
`

type CanaryPart struct {
	Passed        bool   `json:"passed"`
	NotApplicable bool   `json:"not_applicable,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

type CanaryResult struct {
	Harness   string     `json:"harness"`
	Version   string     `json:"version"`
	CheckedAt time.Time  `json:"checked_at"`
	Family    string     `json:"family,omitempty"`
	Process   CanaryPart `json:"process"`
	Hook      CanaryPart `json:"hook"`
	Report    CanaryPart `json:"report"`
}

func (r *CanaryResult) Passed() bool {
	return r.Process.Passed && (r.Hook.Passed || r.Hook.NotApplicable) && r.Report.Passed
}

func (r *CanaryResult) FailedParts() string {
	var failed []string
	for _, part := range []struct {
		name string
		part CanaryPart
	}{{"process", r.Process}, {"hook", r.Hook}, {"report", r.Report}} {
		if !part.part.Passed && !part.part.NotApplicable {
			failed = append(failed, part.name+": "+part.part.Detail)
		}
	}
	return strings.Join(failed, "; ")
}

func canaryFile(name string) string {
	return filepath.Join(config.Home(), "canaries", name+".json")
}

func LoadCanary(name string) (*CanaryResult, error) {
	b, err := os.ReadFile(canaryFile(name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result CanaryResult
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *CanaryResult) save() error {
	path := canaryFile(r.Harness)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".canary-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(b)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// RunCanary is called only by an explicit doctor --canary request. It uses no
// Router or Budget Probe, so the request starts just one small Dispatch.
func RunCanary(env *Env, name string) (result *CanaryResult, err error) {
	h, ok := env.Reg.Get(name)
	if !ok || h.Bin() == "" {
		return nil, fmt.Errorf("Harness %s is not installed", name)
	}
	result = &CanaryResult{
		Harness: name, Version: env.Disc.Harness[name].Version, CheckedAt: time.Now(),
		Process: CanaryPart{Detail: "process did not start"},
		Hook:    CanaryPart{Detail: "no Lane tool call judged by the Maestro hook"},
		Report:  CanaryPart{Detail: "no Report with a valid Status line"},
	}
	if name == "codex" {
		result.Hook = CanaryPart{NotApplicable: true, Detail: "not applicable: the Codex sandbox holds Access"}
	}
	route, routeErr := canaryRoute(env, name)
	if routeErr != nil {
		result.Process.Detail = routeErr.Error()
		return result, result.save()
	}
	result.Family = route.FamilyID
	root, err := os.MkdirTemp("", "maestro-canary-")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(root)) }()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root = resolved
	folder := filepath.Join(config.RunsDir(root), "canary", "check")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, err
	}
	control, err := config.ControlDirForFolder(folder)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(control))
		// This project key belongs only to the scratch folder.
		_ = os.Remove(filepath.Dir(control))
		_ = os.Remove(filepath.Dir(filepath.Dir(control)))
	}()
	if err := os.WriteFile(config.ProjectFile(root), nil, 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, "canary.txt"), []byte("Maestro Canary\n"), 0o600); err != nil {
		return nil, err
	}
	input := filepath.Join(root, "canary.md")
	if err := os.WriteFile(input, []byte(canaryBrief), 0o600); err != nil {
		return nil, err
	}
	task := &Task{
		Project: root, Run: "canary", Task: "check", BriefPath: input, Title: "Check the Harness",
		Category: "triage", Access: brief.AccessRead, Route: route, Folder: folder,
		WallLimit: "2m0s", MaxAttempt: 1, Canary: true,
	}
	if err := task.Save(); err != nil {
		return nil, err
	}
	scratch := *env
	scratch.Root, scratch.Project, scratch.Solo, scratch.Budget = root, &config.Project{}, nil, nil
	rec, err := Execute(&scratch, task)
	if err != nil {
		return nil, err
	}
	started := false
	for _, event := range rec.Events {
		started = started || event.Kind == "start"
	}
	result.Process.Passed = started && rec.State == "ended" && rec.Outcome.ExitCode == 0 &&
		!rec.Outcome.ProcessError && rec.Outcome.Timer == "" && rec.Outcome.Outcome != OutcomeCancelled
	if result.Process.Passed {
		result.Process.Detail = "process started and ended with no error"
	} else if started {
		result.Process.Detail = fmt.Sprintf("exit %d", rec.Outcome.ExitCode)
		if rec.Outcome.ProcessError {
			result.Process.Detail += "; Harness reported a process error"
		}
		if rec.Outcome.Timer != "" {
			result.Process.Detail += "; " + rec.Outcome.Timer + " limit"
		}
	} else if rec.Outcome.ErrorText != "" {
		result.Process.Detail = rec.Outcome.ErrorText
	}
	if b, err := os.ReadFile(filepath.Join(control, hook.FileJudgment)); err == nil && !result.Hook.NotApplicable {
		var judgment hook.Judgment
		if json.Unmarshal(b, &judgment) == nil && judgment.Harness == name && judgment.Tool != "" && judgment.Verdict != "" && !judgment.At.Before(rec.StartedAt) {
			result.Hook = CanaryPart{Passed: true, Detail: judgment.Tool + ": " + judgment.Verdict}
		}
	}
	if report, found := rec.Report(); found && report.Status != brief.StatusUnshaped {
		result.Report = CanaryPart{Passed: true, Detail: "STATUS: " + report.Status}
	}
	result.CheckedAt = time.Now()
	return result, result.save()
}

func canaryRoute(env *Env, name string) (*router.Route, error) {
	h, _ := env.Reg.Get(name)
	if h.Enforcement(brief.AccessRead) != harness.Enforced {
		return nil, fmt.Errorf("Harness %s has no enforced read shape", name)
	}
	var best *router.Route
	bestCost := math.Inf(1)
	for _, f := range env.Roster.SortedFamilies() {
		if f.Harness != name || !f.Enabled {
			continue
		}
		model, ok := env.Disc.Resolve(name, env.Roster.LaunchModels(f))
		if !ok {
			continue
		}
		level := f.NearestLevel(roster.LevelNone)
		if level == "" || roster.FanOut(f.Levels[level]) {
			continue
		}
		priceModel := model
		if alias, ok := env.Roster.Aliases[model]; ok {
			priceModel = alias
		}
		cost := math.Inf(1)
		if price, ok := env.Roster.Prices[priceModel]; ok {
			cost = price.Input + price.Output
		}
		if best == nil || cost < bestCost {
			bestCost = cost
			best = &router.Route{
				Family: f, FamilyID: f.ID, Harness: name, Model: model, Account: f.Account,
				Tier: f.TierFor(level), Level: level, LevelEstimate: roster.LevelNone,
				Effort: f.Levels[level], Source: "requested", Requested: true,
				Category: "triage", Enforcement: harness.Enforced, ReasonLine: "Canary: " + f.ID,
			}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("Harness %s has no enabled Family with an installed model", name)
	}
	return best, nil
}
