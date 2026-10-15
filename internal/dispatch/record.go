// Package dispatch starts a Lane on a Task, watches it, collects the Report,
// verifies the work, and records the Dispatch in the Run folder and the ledger.
package dispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

// Failure classes.
const (
	FailLane      = "lane error"
	FailTransient = "transient error"
	FailLimit     = "limit hit"
	FailStall     = "stall"
	FailBad       = "bad result"
)

// Outcome values.
const (
	OutcomeVerified   = "verified"
	OutcomeFailed     = "failed"
	OutcomeEscalated  = "escalated"
	OutcomeTimedOut   = "timed_out"
	OutcomeCancelled  = "cancelled"
	OutcomeBlocked    = "blocked"
	OutcomeAnswered   = "answered"   // a read Task whose Report came back
	OutcomeUnverified = "unverified" // an edit Task with no deterministic check
)

// Task is the input half of a Dispatch, saved as task.json so a detached
// supervisor can pick it up.
type Task struct {
	Project    string        `json:"project"`
	Run        string        `json:"run"`
	Task       string        `json:"task"`
	BriefPath  string        `json:"brief_path"`
	Title      string        `json:"title"`
	Tab        string        `json:"tab,omitempty"`
	Category   string        `json:"category"`
	Access     string        `json:"access"`
	Commit     bool          `json:"commit"`
	Touch      []string      `json:"touch,omitempty"`
	Dirs       []string      `json:"dirs,omitempty"`
	Checks     []string      `json:"checks,omitempty"`
	WallLimit  string        `json:"wall_limit"`
	IdleLimit  string        `json:"idle_limit"`
	Route      *router.Route `json:"route"`
	Folder     string        `json:"folder"`
	ClaudeAuto bool          `json:"claude_auto"`
	InSolo     bool          `json:"in_solo"`
	MaxAttempt int           `json:"max_attempts"`
	// Council marks a Seat Dispatch: the Report ends with POSITION, not STATUS.
	Council bool `json:"council,omitempty"`
	// Todo is the Solo todo of this Task, 0 outside Solo.
	Todo int `json:"todo,omitempty"`
	// FirstAttempt lets a re-Route inside a Run keep the attempt count.
	FirstAttempt int `json:"first_attempt,omitempty"`
	// LedgerByRun lets the Run record its final attempt after deciding on escalation.
	LedgerByRun bool `json:"ledger_by_run,omitempty"`
	// Cancel, when closed, stops the running Lane (never serialized).
	Cancel <-chan struct{} `json:"-"`
}

// Record is dispatch.json: the full Dispatch record. The ledger row is a copy.
type Record struct {
	judged    bool      // judge has recorded Changed; the deferred recording stands down
	Kind      string    `json:"kind"`
	Project   string    `json:"project"`
	Run       string    `json:"run"`
	Task      string    `json:"task"`
	Attempt   int       `json:"attempt"`
	State     string    `json:"state"` // running | ended
	Pid       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	DurationS float64   `json:"duration_s"`
	Path      string    `json:"path"`
	SessionID string    `json:"session_id,omitempty"`
	// SoloProcess is the Lane's tab when it ran as one.
	SoloProcess int `json:"solo_process,omitempty"`

	TaskInfo     TaskInfo      `json:"task_info"`
	Route        *router.Route `json:"route"`
	Budget       Budget        `json:"budget"`
	Permission   Permission    `json:"permission"`
	Outcome      Outcome       `json:"outcome"`
	Verification Verification  `json:"verification"`
	Events       []Event       `json:"events,omitempty"`
}

// TaskInfo is the Task group of the ledger row.
type TaskInfo struct {
	Title         string `json:"title"`
	Category      string `json:"category"`
	LevelEstimate string `json:"level_estimate"`
	Access        string `json:"access"`
	Commit        bool   `json:"commit"`
	WallLimit     string `json:"wall_limit"`
}

// Budget is the Budget group: the reading at Route time and the tokens.
type Budget struct {
	Reading *Reading         `json:"reading"`
	Warning string           `json:"warning,omitempty"`
	Tokens  harness.Tokens   `json:"tokens"`
	CostUSD *float64         `json:"cost_usd"`
	Windows []harness.Window `json:"windows,omitempty"`
}

// Reading is one Budget reading in force at Route time.
type Reading struct {
	Used   float64   `json:"used"`
	ReadAt time.Time `json:"read_at"`
	Source string    `json:"source"`
	State  string    `json:"state"`
}

// Permission is the Permission group.
type Permission struct {
	Enforcement string            `json:"enforcement"`
	Flags       []string          `json:"flags"`
	Denials     []harness.Denial  `json:"denials"`
	Blocked     []string          `json:"blocked"`
	Relays      []json.RawMessage `json:"relays"`
}

// Outcome is the Outcome group.
type Outcome struct {
	Status            string  `json:"status"` // done | partial | blocked | unshaped
	Outcome           string  `json:"outcome"`
	FailureClass      string  `json:"failure_class,omitempty"`
	Timer             string  `json:"timer,omitempty"`
	ErrorText         string  `json:"error_text,omitempty"`
	ExitCode          int     `json:"exit_code"`
	LimitConfirmation string  `json:"limit_confirmation,omitempty"`
	SleepJumpS        float64 `json:"sleep_jump_s,omitempty"`
	Nudged            bool    `json:"nudged"`
	ModelRan          string  `json:"model_ran,omitempty"`
	ReportByText      bool    `json:"report_by_text,omitempty"`
}

// Verification is the Verification group.
type Verification struct {
	StartCommit     string               `json:"start_commit,omitempty"`
	CommitRange     string               `json:"commit_range,omitempty"`
	Changed         []string             `json:"changed"`
	TouchViolations []string             `json:"touch_violations"`
	Checks          []verify.CheckResult `json:"checks"`
	Gate            json.RawMessage      `json:"gate,omitempty"`
}

// Event is one timeline entry inside a Dispatch (never a ledger row).
type Event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Text string    `json:"text,omitempty"`
}

// Files in the Run folder.
const (
	FileTask     = "task.json"
	FileRecord   = "dispatch.json"
	FileBrief    = "brief.md"
	FileReport   = "result.md"
	FileOutput   = "output.jsonl"
	FileStderr   = "stderr.log"
	FileAttempts = "attempts" // attempts/<n>/ keeps earlier attempts' files
	FileBaseline = "baseline.json"
)

// Save writes the record to its folder.
func (r *Record) Save() error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(r.Path, FileRecord+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(r.Path, FileRecord))
}

// LoadRecord reads dispatch.json from a Run folder.
func LoadRecord(folder string) (*Record, error) {
	b, err := os.ReadFile(filepath.Join(folder, FileRecord))
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveTask writes task.json.
func (t *Task) Save() error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(t.Folder, FileTask), b, 0o644)
}

// LoadTask reads task.json.
func LoadTask(folder string) (*Task, error) {
	b, err := os.ReadFile(filepath.Join(folder, FileTask))
	if err != nil {
		return nil, err
	}
	var t Task
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Report reads result.md when it exists.
func (r *Record) Report() (*brief.Report, bool) {
	b, err := os.ReadFile(filepath.Join(r.Path, FileReport))
	if err != nil {
		return nil, false
	}
	return brief.ParseReport(string(b)), true
}
