package budget

import (
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/harness"
)

func rules() Rules {
	return Rules{BlockAt: 0.90, WarnAt: 0.75, Reserve: 0.35, StaleAfter: 30 * time.Minute, AssumedHeadroom: 0.50}
}

func TestUnknownIsUsable(t *testing.T) {
	f := &File{Accounts: map[string]*Account{}}
	v := f.Evaluate("grok", rules(), time.Now())
	if v.State != "unknown" || v.Headroom != 0.50 {
		t.Fatalf("%+v", v)
	}
}

func TestReserveBindsTheWeeklyWindow(t *testing.T) {
	f := &File{Accounts: map[string]*Account{}}
	now := time.Now()
	f.Record("claude", []harness.Window{{Kind: "session", Used: 0.10, ResetsAt: now.Add(time.Hour)}, {Kind: "weekly", Used: 0.66, ResetsAt: now.Add(48 * time.Hour)}}, SourceHarvest)
	v := f.Evaluate("claude", rules(), now)
	if v.State != "blocked" {
		t.Fatalf("weekly 0.66 with reserve 0.35 must block: %+v", v)
	}
	f.Record("claude", []harness.Window{{Kind: "weekly", Used: 0.40, ResetsAt: now.Add(48 * time.Hour)}}, SourceHarvest)
	v = f.Evaluate("claude", rules(), now)
	if v.State != "ok" || v.Headroom < 0.24 || v.Headroom > 0.26 {
		t.Fatalf("%+v", v)
	}
}

func TestStaleAndExpired(t *testing.T) {
	f := &File{Accounts: map[string]*Account{}}
	now := time.Now()
	f.Record("codex", []harness.Window{{Kind: "weekly", Used: 0.20, ResetsAt: now.Add(time.Hour)}}, SourceHarvest)
	f.Accounts["codex"].Windows[0].ReadAt = now.Add(-time.Hour)
	if v := f.Evaluate("codex", Rules{BlockAt: 0.9, WarnAt: 0.75, StaleAfter: 30 * time.Minute, AssumedHeadroom: 0.5}, now); v.State != "stale" {
		t.Fatalf("%+v", v)
	}
	if v := f.Evaluate("codex", rules(), now.Add(2*time.Hour)); v.State != "unknown" {
		t.Fatalf("expired reading must be unknown: %+v", v)
	}
	f.RecordManual("codex", 0.30)
	f.Accounts["codex"].Windows[0].ReadAt = now.Add(-3 * time.Hour)
	if v := f.Evaluate("codex", rules(), now); v.State != "ok" {
		t.Fatalf("manual readings never go stale: %+v", v)
	}
}
