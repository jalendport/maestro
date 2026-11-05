package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestRunOnceToolCallLimits(t *testing.T) {
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
				bin := filepath.Join(t.TempDir(), "lane")
				script := "#!/bin/sh\n"
				if stream.harness != "grok" {
					script += "cat >/dev/null\n"
				}
				if mode != "idle" {
					script += fmt.Sprintf("printf '%%s\\n' '%s'\n", stream.start)
				}
				wantTimer, wantOutcome := "", OutcomeAnswered
				switch mode {
				case "finishes":
					// Silence spans several Idle checks while the call runs.
					script += "sleep 3\n" + fmt.Sprintf("printf '%%s\\n' '%s'\n", stream.end)
					script += "printf '## Answer\\nOK\\nSTATUS: done\\n' > \"$MAESTRO_TASK_FOLDER/result.md\"\n"
				case "idle-after-tool":
					script += "sleep 3\n" + fmt.Sprintf("printf '%%s\\n' '%s'\n", stream.end)
					script += "exec sleep 30\n"
					wantTimer, wantOutcome = "idle", OutcomeTimedOut
				case "wall":
					script += "exec sleep 30\n"
					wantTimer, wantOutcome = "wall", OutcomeTimedOut
				case "idle":
					script += "exec sleep 30\n"
					wantTimer, wantOutcome = "idle", OutcomeTimedOut
				}
				if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{stream.harness: bin})}
				task := &Task{Project: root, Folder: folder, Run: "r-test", Task: "abcd", Access: brief.AccessRead,
					Route: &router.Route{Harness: stream.harness}, WallLimit: "10s", IdleLimit: "500ms"}
				if mode == "wall" {
					task.WallLimit = "2500ms"
				}
				started := time.Now()
				rec := runOnce(env, task, &brief.Brief{}, 1, nil)
				if rec.Outcome.Timer != wantTimer || rec.Outcome.Outcome != wantOutcome {
					t.Fatalf("outcome: %+v, want timer %q and outcome %s", rec.Outcome, wantTimer, wantOutcome)
				}
				if mode == "idle-after-tool" && time.Since(started) < 3500*time.Millisecond {
					t.Fatal("Idle limit did not restart after the tool call ended")
				}
			})
		}
	}
}
