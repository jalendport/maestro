// Package run is the per-Run supervisor: it turns a Plan file into Tasks,
// schedules them under the concurrency caps, drives each through dispatch,
// handles the escalation path and the Run pause, and writes the Run state,
// the Run report, and the ledger row.
package run

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

// Run states.
const (
	StateHeld      = "held" // breakdown waits for approval
	StateLive      = "live"
	StatePaused    = "paused"
	StateFinished  = "finished"
	StateCancelled = "cancelled"
)

// Task states inside a Run.
const (
	TaskWaiting   = "waiting"
	TaskRunning   = "running"
	TaskDone      = "done"    // verified, answered, or unverified
	TaskBlocked   = "blocked" // needs the Orchestrator
	TaskFailed    = "failed"  // escalation path exhausted; needs the Orchestrator
	TaskSkipped   = "skipped"
	TaskCancelled = "cancelled"
)

// TaskState is one Task's live record in run.json.
type TaskState struct {
	Number            int           `json:"number"`
	ID                string        `json:"id"`
	Title             string        `json:"title"`
	BriefPath         string        `json:"brief_path"`
	Access            string        `json:"access"`
	Category          string        `json:"category"`
	BlockedBy         []string      `json:"blocked_by"`
	State             string        `json:"state"`
	DependencyBlocked bool          `json:"dependency_blocked,omitempty"`
	Waiting           string        `json:"waiting,omitempty"`
	Route             *router.Route `json:"route,omitempty"`
	Provisional       bool          `json:"provisional"`
	Attempt           int           `json:"attempt"`
	Outcome           string        `json:"outcome,omitempty"`
	Status            string        `json:"status,omitempty"`
	Error             string        `json:"error,omitempty"`
	StartedAt         time.Time     `json:"started_at,omitempty"`
	EndedAt           time.Time     `json:"ended_at,omitempty"`
	LastOutput        time.Time     `json:"last_output,omitempty"`
	Folder            string        `json:"folder,omitempty"`
	Excluded          []string      `json:"excluded_families,omitempty"`
	Escalations       int           `json:"escalations"`
	// Resume answers from the Orchestrator, consumed on the next start.
	ResumeLane  string `json:"resume_lane,omitempty"`
	ResumeBrief string `json:"resume_brief,omitempty"`
	ResumeSkip  bool   `json:"resume_skip,omitempty"`
	Cancel      bool   `json:"cancel,omitempty"`
}

// State is run.json.
type State struct {
	Run         string       `json:"run"`
	Project     string       `json:"project"`
	Plan        string       `json:"plan"`
	Title       string       `json:"title"`
	State       string       `json:"state"`
	Pid         int          `json:"pid,omitempty"`
	StartedAt   time.Time    `json:"started_at"`
	EndedAt     time.Time    `json:"ended_at,omitempty"`
	Tasks       []*TaskState `json:"tasks"`
	Approval    []string     `json:"approval_needed,omitempty"`
	PauseReason string       `json:"pause_reason,omitempty"`
	Councils    []string     `json:"councils,omitempty"`
	Awake       bool         `json:"caffeinate"`
	Folder      string       `json:"folder"`
	Lane        string       `json:"lane,omitempty"` // --lane for every Task
	Level       roster.Level `json:"level,omitempty"`
	Floor       roster.Tier  `json:"floor,omitempty"`
	Ceiling     roster.Tier  `json:"ceiling,omitempty"`
	CouncilSkip string       `json:"council_skip,omitempty"`
}

const FileState = "run.json"
const FileReport = "report.md"

// Save rewrites run.json atomically.
func (s *State) Save() error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Folder, FileState+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Folder, FileState))
}

// Load reads run.json from a Run folder.
func Load(folder string) (*State, error) {
	b, err := os.ReadFile(filepath.Join(folder, FileState))
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Task finds a Task by number, id, or title.
func (s *State) Task(ref string) (*TaskState, bool) {
	if n, err := strconv.Atoi(ref); err == nil {
		for _, t := range s.Tasks {
			if t.Number == n {
				return t, true
			}
		}
	}
	for _, t := range s.Tasks {
		if t.ID == ref || t.Title == ref {
			return t, true
		}
	}
	return nil, false
}

// Counts summarizes Task states.
func (s *State) Counts() (done, blocked, failed, running, waiting, skipped int) {
	for _, t := range s.Tasks {
		switch t.State {
		case TaskDone:
			done++
		case TaskBlocked:
			blocked++
		case TaskFailed:
			failed++
		case TaskRunning:
			running++
		case TaskWaiting:
			waiting++
		case TaskSkipped, TaskCancelled:
			skipped++
		}
	}
	return
}

// Index is ~/.maestro/runs/<project-key>-<run-id>.json: how other Runs see this one.
type Index struct {
	Run       string    `json:"run"`
	Project   string    `json:"project"`
	Folder    string    `json:"folder"`
	Pid       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

func indexPath(project, run string) string {
	root, err := filepath.Abs(project)
	if err != nil {
		root = filepath.Clean(project)
	}
	key := sha256.Sum256([]byte(root))
	return filepath.Join(config.RunsIndexDir(), fmt.Sprintf("%x-%s.json", key, run))
}

// WriteIndex registers a live Run.
func WriteIndex(s *State) error {
	if err := config.EnsureHome(); err != nil {
		return err
	}
	b, _ := json.Marshal(Index{Run: s.Run, Project: s.Project, Folder: s.Folder, Pid: os.Getpid(), StartedAt: s.StartedAt})
	return os.WriteFile(indexPath(s.Project, s.Run), b, 0o644)
}

// RemoveIndex unregisters a Run.
func RemoveIndex(project, run string) { _ = os.Remove(indexPath(project, run)) }

// LiveRuns lists Runs whose supervisor is alive; dead entries are removed.
func LiveRuns() []Index {
	entries, _ := os.ReadDir(config.RunsIndexDir())
	var out []Index
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(config.RunsIndexDir(), e.Name()))
		if err != nil {
			continue
		}
		var idx Index
		if json.Unmarshal(b, &idx) != nil || idx.Pid == 0 {
			continue
		}
		if syscall.Kill(idx.Pid, 0) != nil {
			_ = os.Remove(filepath.Join(config.RunsIndexDir(), e.Name()))
			continue
		}
		out = append(out, idx)
	}
	return out
}

// LiveRunFor returns the live Run of a project, if any.
func LiveRunFor(project string) (Index, bool) {
	for _, idx := range LiveRuns() {
		if idx.Project == project {
			return idx, true
		}
	}
	return Index{}, false
}

// ErrNoRun means no run.json was found.
var ErrNoRun = errors.New("no run found")

// NewestRun returns the newest Run folder in a project.
func NewestRun(root string) (string, error) {
	entries, err := dispatch.RunFolders(root)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, entry := range entries {
		p := filepath.Join(config.RunsDir(root), entry.Name())
		if _, err := os.Stat(filepath.Join(p, FileState)); err == nil {
			return p, nil
		}
	}
	return "", ErrNoRun
}
