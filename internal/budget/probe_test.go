package budget

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexProbeKeepsReportedWindowDurations(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	bin := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
read line
read line
read line
printf '%s\n' '{"id":2,"result":{"rateLimits":{"planType":"test","primary":{"usedPercent":70,"windowDurationMins":120,"resetsAt":2000000000},"secondary":{"usedPercent":30,"windowDurationMins":10080,"resetsAt":2000500000}}}}'
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	limits, err := ReadCodex(bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(limits.Windows) != 2 || limits.Windows[0].Duration != 2*time.Hour || limits.Windows[1].Duration != 7*24*time.Hour {
		t.Fatalf("windows: %+v", limits.Windows)
	}
	file, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	file.Record("codex", limits.Windows, SourceProbe)
	if err := file.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := loaded.Snapshot("codex")
	if snapshot == nil || len(snapshot.Windows) != 2 || snapshot.Windows[0].Duration != 2*time.Hour || WindowName(snapshot.Windows[0]) != "2h" {
		t.Fatalf("stored windows: %+v", snapshot)
	}
}

func TestCodexHarvestUsesConfigHomeAndWindowDurations(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	folder := filepath.Join(dir, "sessions", "2026", "10", "13")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"payload":{"rate_limits":{"primary":{"used_percent":70,"window_minutes":120,"resets_at":2000000000},"secondary":{"used_percent":30,"window_minutes":10080,"resets_at":2000500000}}}}`
	if err := os.WriteFile(filepath.Join(folder, "rollout-test-thread.jsonl"), []byte(data+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	windows, err := HarvestCodexRollout("test-thread")
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 2 || windows[0].Duration != 2*time.Hour || windows[1].Duration != 7*24*time.Hour {
		t.Fatalf("windows: %+v", windows)
	}
}

func TestFindCodexRolloutUsesLaunchTimeAndWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	folder := filepath.Join(dir, "sessions", "2026", "10", "13")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, session := range []struct {
		id, cwd string
		at      time.Time
	}{
		{"old", "/run", start.Add(-time.Minute)},
		{"first", "/run", start.Add(time.Second)},
		{"newest", "/run", start.Add(2 * time.Second)},
		{"other-task", "/other-run", start.Add(3 * time.Second)},
	} {
		data := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q,"timestamp":%q}}`, session.id, session.cwd, session.at.Format(time.RFC3339Nano))
		data += "\n" + `{"payload":{"rate_limits":{"primary":{"used_percent":42,"window_minutes":120,"resets_at":2000000000}}}}` + "\n"
		if err := os.WriteFile(filepath.Join(folder, "rollout-"+session.id+".jsonl"), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id, err := FindCodexRollout(start, "/run")
	if err != nil || id != "newest" {
		t.Fatalf("session = %q, error = %v", id, err)
	}
	ws, err := HarvestCodexRollout(id)
	if err != nil || len(ws) != 1 || ws[0].Used != .42 {
		t.Fatalf("harvest: %+v, %v", ws, err)
	}
	if _, err := FindCodexRollout(start.Add(time.Minute), "/run"); err == nil {
		t.Fatal("accepted a session from before the launch")
	}
	if _, err := FindCodexRollout(start, "/missing"); err == nil {
		t.Fatal("accepted a different workspace")
	}
}

func TestClaudeProbeHonoursDeadline(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ProbeClaude(ctx, bin, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline took %s", elapsed)
	}
}
