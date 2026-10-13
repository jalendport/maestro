package budget

import (
	"bufio"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
)

func rules() Rules {
	return Rules{BlockAt: 0.90, WarnAt: 0.75, Reserve: 0.35, StaleAfter: 30 * time.Minute, AssumedHeadroom: 0.50}
}

func TestSaveMergesDifferentAccounts(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	first.Record("codex", []harness.Window{{Kind: "weekly", Used: .2}}, SourceProbe)
	second.Record("claude", []harness.Window{{Kind: "session", Used: .3}}, SourceHarvest)
	for _, f := range []*File{first, second} {
		if err := f.Save(); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Accounts) != 2 || loaded.Snapshot("codex").Windows[0].Used != .2 || loaded.Snapshot("claude").Windows[0].Used != .3 {
		t.Fatalf("accounts: %+v", loaded.Accounts)
	}
}

func TestSaveMergesReadingsBlocksAndExplicitCredits(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	now := time.Now()
	seed := &File{Accounts: map[string]*Account{"codex": {
		Windows: []Window{{Kind: "weekly", Used: .1, ReadAt: now}}, Credits: 3, Plan: "pro",
	}}}
	if err := seed.Save(); err != nil {
		t.Fatal(err)
	}
	stale, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	fresh.Record("codex", []harness.Window{{Kind: "weekly", Used: .8}}, SourceHarvest)
	fresh.SetCredits("codex", 2, "plus")
	fresh.Block("codex", now.Add(2*time.Hour), "limit")
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	stale.Record("codex", []harness.Window{{Kind: "session", Used: .4}}, SourceProbe)
	stale.Block("codex", now.Add(time.Hour), "shorter limit")
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	a := stale.Snapshot("codex")
	if len(a.Windows) != 2 || a.Windows[1].Used != .8 || a.Credits != 2 || a.Plan != "plus" || !a.BlockedUntil.Equal(now.Add(2*time.Hour)) || a.BlockReason != "limit" {
		t.Fatalf("merged account: %+v", a)
	}
	stale.SetCredits("codex", 0, "free")
	stale.Unblock("codex")
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	// An unchanged copy must not restore credits or the cleared block.
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	a = fresh.Snapshot("codex")
	if a.Credits != 0 || a.Plan != "free" || a.BlockedUntil != nil {
		t.Fatalf("stale save restored state: %+v", a)
	}
	// An explicit set wins even when its value matches the original snapshot.
	fresh.SetCredits("codex", 0, "free")
	stale.SetCredits("codex", 1, "plus")
	stale.Block("codex", now.Add(3*time.Hour), "new limit")
	if err := stale.Save(); err != nil {
		t.Fatal(err)
	}
	fresh.Unblock("codex")
	if err := fresh.Save(); err != nil {
		t.Fatal(err)
	}
	a = fresh.Snapshot("codex")
	if a.Credits != 0 || a.Plan != "free" || a.BlockedUntil == nil || a.BlockReason != "new limit" {
		t.Fatalf("explicit credits or concurrent block lost: %+v", a)
	}
}

func TestSaveRejectsInvalidStoredJSON(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("{broken")
	if err := os.WriteFile(config.BudgetFile(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if loaded, err := Load(); err != nil || len(loaded.Accounts) != 0 {
		t.Fatalf("invalid Budget must still load as unknown: %+v, %v", loaded, err)
	}
	f.RecordManual("codex", .2)
	if err := f.Save(); err == nil {
		t.Fatal("overwrote invalid stored JSON")
	}
	stored, err := os.ReadFile(config.BudgetFile())
	if err != nil || string(stored) != string(data) {
		t.Fatalf("stored file: %q, %v", stored, err)
	}
}

func TestSaveProcess(t *testing.T) {
	if id := os.Getenv("MAESTRO_BUDGET_TEST_ACCOUNT"); id != "" {
		f, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		f.RecordManual(id, .2)
		if _, err := os.Stdout.WriteString("ready\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Save(); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Setenv("MAESTRO_HOME", t.TempDir())
	lock, err := os.OpenFile(config.BudgetFile()+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var children []*exec.Cmd
	var done []chan error
	for _, id := range []string{"codex", "claude"} {
		cmd := exec.Command(self, "-test.run=^TestSaveProcess$")
		cmd.Env = append(os.Environ(), "MAESTRO_BUDGET_TEST_ACCOUNT="+id)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatalf("child readiness: %q, %v", line, err)
		}
		children = append(children, cmd)
		ch := make(chan error, 1)
		go func() { ch <- cmd.Wait() }()
		done = append(done, ch)
	}
	for i, ch := range done {
		select {
		case err := <-ch:
			t.Fatalf("child %d saved without lock: %v", i, err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	for i, ch := range done {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("child %d: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("child %d did not save", i)
		}
	}
	f, err := Load()
	if err != nil || f == nil || len(f.Accounts) != len(children) {
		t.Fatalf("accounts after process saves: %+v, %v", f, err)
	}
}

func TestViewDoesNotAliasLiveWindow(t *testing.T) {
	f := &File{Accounts: map[string]*Account{}}
	now := time.Now()
	f.Record("codex", []harness.Window{{Kind: "weekly", Used: .2}}, SourceProbe)
	v := f.Evaluate("codex", rules(), now)
	if v.Tight == nil {
		t.Fatal("missing tight window")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			f.Record("codex", []harness.Window{{Kind: "weekly", Used: .8}}, SourceHarvest)
		}
	}()
	for i := 0; i < 1000; i++ {
		if v.Tight.Used != .2 || v.Tight.Source != SourceProbe {
			t.Errorf("view changed: %+v", v.Tight)
			break
		}
	}
	wg.Wait()
	v.Tight.Used = .9
	if got := f.Snapshot("codex").Windows[0].Used; got != .8 {
		t.Fatalf("view changed live state: %v", got)
	}
}

func TestBlockWinsWithoutReading(t *testing.T) {
	f := &File{Accounts: map[string]*Account{}}
	now := time.Now()
	f.Block("codex", now.Add(time.Hour), "limit")
	if v := f.Evaluate("codex", rules(), now); v.State != "blocked" || v.Headroom != 0 {
		t.Fatalf("block ignored: %+v", v)
	}
	if v := f.Evaluate("codex", rules(), now.Add(2*time.Hour)); v.State != "unknown" {
		t.Fatalf("expired block still binds: %+v", v)
	}
}

func TestReserveNeverRaisesBlockPoint(t *testing.T) {
	for _, reserve := range []float64{0, .05, .35} {
		f := &File{Accounts: map[string]*Account{}}
		f.Record("codex", []harness.Window{{Kind: "weekly", Used: .93}}, SourceProbe)
		r := rules()
		r.Reserve = reserve
		if v := f.Evaluate("codex", r, time.Now()); v.State != "blocked" {
			t.Fatalf("reserve %v weakened block point: %+v", reserve, v)
		}
	}
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
