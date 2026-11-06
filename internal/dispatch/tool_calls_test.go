package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestRunOnceToolCallLimits(t *testing.T) {
	fastDispatchTimers(t)
	// One Lane script for all cases: the first start of a new script file is
	// slow on macOS, so each case sets the mode and the stream lines in the
	// environment of the same script.
	bin := filepath.Join(t.TempDir(), "lane")
	script := `#!/bin/sh
if [ "$TEST_TOOL_HARNESS" != grok ]; then read pointer; fi
if [ "$TEST_TOOL_MODE" != idle ]; then printf '%s\n' "$TEST_TOOL_START"; fi
case "$TEST_TOOL_MODE" in
finishes)
	# Silence spans several Idle checks while the call runs.
	while [ ! -f "$TEST_TOOL_RELEASE" ]; do sleep 0.002; done
	printf '%s\n' "$TEST_TOOL_END"
	printf '## Answer\nOK\nSTATUS: done\n' > "$MAESTRO_TASK_FOLDER/result.md"
	;;
idle-after-tool)
	while [ ! -f "$TEST_TOOL_RELEASE" ]; do sleep 0.002; done
	printf '%s\n' "$TEST_TOOL_END"
	exec sleep 30
	;;
*)
	exec sleep 30
	;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, stream := range []struct {
		harness string
		start   string
		end     string
	}{
		{"claude", `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a","name":"Bash"}]}}`, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a","content":"ok"}]}}`},
		{"codex", `{"type":"item.started","item":{"id":"a","type":"command_execution"}}`, `{"type":"item.completed","item":{"id":"a","type":"command_execution"}}`},
		{"grok", `{"type":"tool_call","toolCallId":"a","status":"pending"}`, `{"type":"tool_call_update","toolCallId":"a","status":"completed"}`},
	} {
		for _, mode := range []string{"finishes", "wall", "idle", "idle-after-tool"} {
			t.Run(stream.harness+"/"+mode, func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				root, folder := t.TempDir(), t.TempDir()
				clock := newToolClock(t)
				release := filepath.Join(folder, "release")
				t.Setenv("TEST_TOOL_RELEASE", release)
				t.Setenv("TEST_TOOL_HARNESS", stream.harness)
				t.Setenv("TEST_TOOL_MODE", mode)
				t.Setenv("TEST_TOOL_START", stream.start)
				t.Setenv("TEST_TOOL_END", stream.end)
				wantTimer, wantOutcome := "", OutcomeAnswered
				switch mode {
				case "idle-after-tool", "idle":
					wantTimer, wantOutcome = "idle", OutcomeTimedOut
				case "wall":
					wantTimer, wantOutcome = "wall", OutcomeTimedOut
				}
				env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{stream.harness: bin})}
				task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessRead,
					Route: &router.Route{Harness: stream.harness}, WallLimit: "10s", IdleLimit: "500ms"}
				if mode == "wall" {
					task.WallLimit = "2500ms"
				}
				cancel := make(chan struct{})
				task.Cancel = cancel
				done := make(chan error, 1)
				go func() {
					err := clock.drive(mode, release)
					if err != nil {
						close(cancel)
					}
					done <- err
				}()
				started := clock.now()
				rec := runOnce(env, task, &brief.Brief{}, 1, nil)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if rec.Outcome.Timer != wantTimer || rec.Outcome.Outcome != wantOutcome {
					t.Fatalf("outcome: %+v, want timer %q and outcome %s", rec.Outcome, wantTimer, wantOutcome)
				}
				if mode == "idle-after-tool" && clock.now().Sub(started) < 3500*time.Millisecond {
					t.Fatal("Idle limit did not restart after the tool call ended")
				}
			})
		}
	}
}

// toolClock advances only after the stream parser records activity. The child
// process still runs, but its tool call and silence use virtual time.
type toolClock struct {
	mu    sync.Mutex
	at    time.Time
	reads chan struct{}
	ticks chan time.Time
}

func newToolClock(t *testing.T) *toolClock {
	t.Helper()
	c := &toolClock{at: time.Now(), reads: make(chan struct{}, 8), ticks: make(chan time.Time)}
	oldNow, oldTicks := dispatchNow, dispatchTicks
	dispatchNow = func() time.Time {
		at := c.now()
		c.reads <- struct{}{}
		return at
	}
	dispatchTicks = c.ticks
	t.Cleanup(func() { dispatchNow, dispatchTicks = oldNow, oldTicks })
	return c
}

func (c *toolClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *toolClock) drive(mode, release string) error {
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	read := func() bool {
		select {
		case <-c.reads:
			return true
		case <-deadline.C:
			return false
		}
	}
	tick := func(step time.Duration) bool {
		c.mu.Lock()
		c.at = c.at.Add(step)
		at := c.at
		c.mu.Unlock()
		select {
		case c.ticks <- at:
			return true
		case <-deadline.C:
			return false
		}
	}
	// Record start, initial Idle time, then the first parsed tool event.
	for i := 0; i < 2; i++ {
		if !read() {
			return fmt.Errorf("Dispatch clock did not start")
		}
	}
	if mode != "idle" && !read() {
		return fmt.Errorf("tool call did not start")
	}
	if mode == "wall" || mode == "idle" || mode == "idle-after-output" {
		step := 2500*time.Millisecond + time.Nanosecond
		if mode != "wall" {
			step = 500*time.Millisecond + time.Nanosecond
		}
		if !tick(step) {
			return fmt.Errorf("Dispatch did not read the limit tick")
		}
		return nil
	}
	// The next receive proves that the first tick did not stop the open call.
	if !tick(3*time.Second) || !tick(0) {
		return fmt.Errorf("Idle limit stopped an open tool call")
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		return err
	}
	if mode == "idle-after-tool" {
		if !read() || !tick(500*time.Millisecond+time.Nanosecond) {
			return fmt.Errorf("Idle limit did not restart after tool output")
		}
	}
	return nil
}
