// Package budget keeps one Budget record per Account: its usage windows,
// where each reading came from, and whether the Account is blocked. The
// Router removes blocked Accounts (hard rule 3); nothing here estimates in
// flight or projects burn.
package budget

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
)

// Sources of a reading.
const (
	SourceHarvest  = "harvest"
	SourceProbe    = "probe"
	SourceManual   = "manual"
	SourceComputed = "computed"
	SourceUnknown  = "unknown"
)

// Window is one stored reading for one usage window.
type Window struct {
	Duration time.Duration `json:"duration,omitempty"`
	Kind     string        `json:"kind"` // session | weekly | balance
	Used     float64       `json:"used"` // 0..1
	ResetsAt time.Time     `json:"resets_at"`
	ReadAt   time.Time     `json:"read_at"`
	Source   string        `json:"source"`
}

// Account is one Account's record.
type Account struct {
	Windows      []Window   `json:"windows"`
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
	BlockReason  string     `json:"block_reason,omitempty"`
	Credits      int        `json:"reset_credits,omitempty"`
	Plan         string     `json:"plan,omitempty"`
}

// File is ~/.maestro/budget.json. Dispatches, Seats, and probes share one
// File across goroutines, so every method takes the lock.
type File struct {
	Accounts   map[string]*Account `json:"accounts"`
	UpdatedAt  time.Time           `json:"updated_at"`
	mu         sync.Mutex
	baseline   map[string]*Account
	creditsSet map[string]bool
}

// Load reads the file; missing is empty.
func Load() (*File, error) {
	f, err := readFile()
	var syntax *json.SyntaxError
	var mismatch *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &mismatch) {
		// Keep an unreadable Budget usable as unknown; Save still refuses to replace it.
		f, err = &File{Accounts: map[string]*Account{}}, nil
	}
	if err == nil {
		f.baseline = copyAccounts(f.Accounts)
	}
	return f, err
}

func readFile() (*File, error) {
	b, err := os.ReadFile(config.BudgetFile())
	if errors.Is(err, os.ErrNotExist) {
		return &File{Accounts: map[string]*Account{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.Accounts == nil {
		f.Accounts = map[string]*Account{}
	}
	return &f, nil
}

// Save merges this process's changes under a process lock, then writes atomically.
func (f *File) Save() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := config.EnsureHome(); err != nil {
		return err
	}
	lock, err := os.OpenFile(config.BudgetFile()+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	merged, err := readFile()
	if err != nil {
		return err
	}
	now := time.Now()
	for id, local := range f.Accounts {
		if local == nil {
			continue
		}
		current := merged.account(id)
		for _, w := range local.Windows {
			newest := true
			for _, stored := range current.Windows {
				if stored.Kind == w.Kind && !w.ReadAt.After(stored.ReadAt) {
					newest = false
					break
				}
			}
			if newest {
				current.Windows = replace(current.Windows, w)
			}
		}
		sort.Slice(current.Windows, func(i, j int) bool { return current.Windows[i].Kind < current.Windows[j].Kind })
		base := f.baseline[id]
		if f.creditsSet[id] || (base == nil && (local.Credits != 0 || local.Plan != "")) ||
			(base != nil && (local.Credits != base.Credits || local.Plan != base.Plan)) {
			current.Credits, current.Plan = local.Credits, local.Plan
		}
		if base == nil || !sameBlock(local, base) {
			localFuture := local.BlockedUntil != nil && now.Before(*local.BlockedUntil)
			currentFuture := current.BlockedUntil != nil && now.Before(*current.BlockedUntil)
			if !currentFuture || (localFuture && local.BlockedUntil.After(*current.BlockedUntil)) ||
				(!localFuture && base != nil && sameBlock(current, base)) {
				current.BlockedUntil, current.BlockReason = local.BlockedUntil, local.BlockReason
			}
		}
	}
	merged.UpdatedAt = now
	b, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(config.BudgetFile()), ".budget.json-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), config.BudgetFile()); err != nil {
		return err
	}
	f.Accounts, f.UpdatedAt = merged.Accounts, merged.UpdatedAt
	f.baseline = copyAccounts(f.Accounts)
	f.creditsSet = nil
	return nil
}

func sameBlock(a, b *Account) bool {
	if a.BlockReason != b.BlockReason {
		return false
	}
	if a.BlockedUntil == nil || b.BlockedUntil == nil {
		return a.BlockedUntil == nil && b.BlockedUntil == nil
	}
	return a.BlockedUntil.Equal(*b.BlockedUntil)
}

func copyAccounts(accounts map[string]*Account) map[string]*Account {
	copies := make(map[string]*Account, len(accounts))
	for id, a := range accounts {
		if a != nil {
			c := *a
			c.Windows = append([]Window(nil), a.Windows...)
			if a.BlockedUntil != nil {
				until := *a.BlockedUntil
				c.BlockedUntil = &until
			}
			copies[id] = &c
		}
	}
	return copies
}

// Credits records the reset credits and plan of an Account.
func (f *File) SetCredits(id string, credits int, plan string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.account(id)
	a.Credits = credits
	a.Plan = plan
	if f.creditsSet == nil {
		f.creditsSet = map[string]bool{}
	}
	f.creditsSet[id] = true
}

// Unblock clears a block.
func (f *File) Unblock(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.account(id)
	a.BlockedUntil = nil
	a.BlockReason = ""
}

// Snapshot returns a copy of one Account for display.
func (f *File) Snapshot(id string) *Account {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.Accounts[id]
	if a == nil {
		return nil
	}
	c := *a
	c.Windows = append([]Window(nil), a.Windows...)
	return &c
}

func (f *File) account(id string) *Account {
	a := f.Accounts[id]
	if a == nil {
		a = &Account{}
		f.Accounts[id] = a
	}
	return a
}

// Record stores windows for an Account, replacing earlier readings of the
// same kind. A fresh reading under the block point clears a block.
func (f *File) Record(id string, windows []harness.Window, source string) {
	if len(windows) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.account(id)
	now := time.Now()
	for _, w := range windows {
		a.Windows = replace(a.Windows, Window{Kind: w.Kind, Duration: w.Duration, Used: w.Used, ResetsAt: w.ResetsAt, ReadAt: now, Source: source})
	}
	sort.Slice(a.Windows, func(i, j int) bool { return a.Windows[i].Kind < a.Windows[j].Kind })
}

// RecordManual stores one manual reading on every window of the Account, or
// a weekly window when none exists. It lasts until the window resets.
func (f *File) RecordManual(id string, used float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.account(id)
	if len(a.Windows) == 0 {
		a.Windows = []Window{{Kind: "weekly", Used: used, ReadAt: time.Now(), Source: SourceManual, ResetsAt: time.Now().Add(7 * 24 * time.Hour)}}
		return
	}
	for i := range a.Windows {
		a.Windows[i].Used = used
		a.Windows[i].ReadAt = time.Now()
		a.Windows[i].Source = SourceManual
	}
	a.BlockedUntil = nil
	a.BlockReason = ""
}

// Block marks an Account blocked until a time.
func (f *File) Block(id string, until time.Time, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.account(id)
	a.BlockedUntil = &until
	a.BlockReason = reason
}

func replace(ws []Window, w Window) []Window {
	for i := range ws {
		if ws[i].Kind == w.Kind {
			ws[i] = w
			return ws
		}
	}
	return append(ws, w)
}

// Rules are the thresholds a View applies.
type Rules struct {
	BlockAt         float64
	WarnAt          float64
	Reserve         float64 // share of the weekly window kept for the Orchestrator
	StaleAfter      time.Duration
	AssumedHeadroom float64
}

// View is what the Router reads for one Account.
type View struct {
	Headroom float64 // 0..1, above the reserve
	State    string  // ok | warning | blocked | unknown | stale
	Reason   string
	Tight    *Window // the window that set the headroom
	Stale    bool
}

// Evaluate derives the Router's view: the tightest window decides; a reading
// older than StaleAfter is stale (a probe may refresh it); no reading is
// unknown and usable with the assumed headroom.
func (f *File) Evaluate(id string, r Rules, now time.Time) View {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := f.Accounts[id]
	if a == nil {
		return View{Headroom: r.AssumedHeadroom, State: SourceUnknown, Reason: "budget unknown"}
	}
	if a.BlockedUntil != nil && now.Before(*a.BlockedUntil) {
		return View{Headroom: 0, State: "blocked", Reason: "blocked until " + a.BlockedUntil.Local().Format("15:04") + ": " + a.BlockReason}
	}
	if len(a.Windows) == 0 {
		return View{Headroom: r.AssumedHeadroom, State: SourceUnknown, Reason: "budget unknown"}
	}
	v := View{Headroom: 1, State: "ok"}
	for i := range a.Windows {
		w := a.Windows[i]
		if !w.ResetsAt.IsZero() && now.After(w.ResetsAt) {
			// The window reset since the reading: the reading no longer binds.
			v.Stale = true
			continue
		}
		if w.Source != SourceManual && r.StaleAfter > 0 && now.Sub(w.ReadAt) > r.StaleAfter {
			v.Stale = true
		}
		head := 1 - w.Used
		block := r.BlockAt
		if w.Kind == "weekly" && r.Reserve > 0 {
			head = 1 - w.Used - r.Reserve
			block = min(block, 1-r.Reserve)
		}
		if head < v.Headroom {
			v.Headroom = head
			v.Tight = &w
		}
		switch {
		case w.Used >= block:
			v.State = "blocked"
			v.Reason = windowWord(&w) + " at " + pct(w.Used)
		case w.Used >= r.WarnAt && v.State == "ok":
			v.State = "warning"
			v.Reason = windowWord(&w) + " at " + pct(w.Used)
		}
	}
	if v.Headroom < 0 {
		v.Headroom = 0
	}
	if v.Tight == nil && v.Stale {
		return View{Headroom: r.AssumedHeadroom, State: SourceUnknown, Reason: "reading expired", Stale: true}
	}
	if v.Stale && v.State == "ok" {
		v.State = "stale"
		v.Reason = "reading is stale"
	}
	return v
}

func (w Window) Length() time.Duration {
	if w.Duration > 0 {
		return w.Duration
	}
	switch w.Kind {
	case "session":
		return 5 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	}
	return 0
}

func WindowName(w Window) string {
	length := w.Length()
	switch {
	case length <= 0:
		return w.Kind
	case length%(24*time.Hour) == 0:
		return itoa(int(length/(24*time.Hour))) + "d"
	case length%time.Hour == 0:
		return itoa(int(length/time.Hour)) + "h"
	case length%time.Minute == 0:
		return itoa(int(length/time.Minute)) + "m"
	}
	return length.String()
}

func windowWord(w *Window) string { return WindowName(*w) + " window" }

func pct(x float64) string { return json.Number(formatPct(x)).String() + "%" }

func formatPct(x float64) string {
	n := int(x*100 + 0.5)
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
