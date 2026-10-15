package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func seatTestEnv(t *testing.T, report string) *dispatch.Env {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("SOLO_PROCESS_ID", "")
	root := t.TempDir()
	bin := filepath.Join(t.TempDir(), "lane")
	payload := filepath.Join(t.TempDir(), "report")
	if err := os.WriteFile(payload, []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_SEAT_REPORT", payload)
	script := "#!/bin/sh\ncat >/dev/null\ncp \"$MAESTRO_TEST_SEAT_REPORT\" \"$MAESTRO_TASK_FOLDER/result.md\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// On Linux the Claude pre-flight wants the sandbox tools on PATH.
	tools := t.TempDir()
	for _, name := range []string{"bwrap", "socat"} {
		if err := os.WriteFile(filepath.Join(tools, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	d, _, err := config.ParseFile(roster.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	r := roster.Merge(d, &config.File{})
	r.Router.Backend = "rules"
	off := false
	r.Failure.KeepAwake = &off
	return &dispatch.Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"claude": bin}),
		Disc: &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{"claude": {
			Path: bin, Version: "2.1.283", Models: []harness.Model{{ID: r.Families["claude-sonnet"].Models[0]}},
		}}}}
}

func TestChallengeRunsBeforeConfirmedEarlyStop(t *testing.T) {
	env := seatTestEnv(t, "## Answer\nPASS\n## Findings\n- none\n## Concerns\n- none\nPOSITION: confirmed\n")
	c, err := Create(env, Opts{Question: "Review", Lanes: []string{"claude-sonnet"}, Rounds: 4})
	if err != nil {
		t.Fatal(err)
	}
	c, err = Start(env, c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if c.RoundsRun != 2 || len(c.Seats[0].Rounds) != 2 || c.Seats[0].Rounds[1].Status != "ok" {
		t.Fatalf("challenge did not run before stop: %+v, rounds %+v", c, c.Seats[0].Rounds)
	}
	path := filepath.Join(c.Path, "round-2", "claude-sonnet", dispatch.FileBrief)
	text, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(text), "This is a challenge round") {
		t.Fatalf("challenge Brief: %s, %v", text, err)
	}
}

func TestBlockedAndUnpositionedSeatsAreDropped(t *testing.T) {
	for _, ending := range []string{"STATUS: blocked\n", "STATUS: done\n"} {
		t.Run(strings.TrimSpace(ending), func(t *testing.T) {
			env := seatTestEnv(t, "## Answer\nCannot answer\n## Concerns\nDenied command\n"+ending)
			c, err := Create(env, Opts{Question: "Review", Lanes: []string{"claude-sonnet"}, Rounds: 2})
			if err != nil {
				t.Fatal(err)
			}
			c, err = Start(env, c.Path)
			if err != nil {
				t.Fatal(err)
			}
			if r := last(c.Seats[0]); r.Status != "dropped" || r.Position != "" || r.Report != "" || allConfirmed(c, 1) || !c.SingleOpinion {
				t.Fatalf("unavailable Seat counted as answer: %+v", r)
			}
		})
	}
}

func TestCouncilContextPersistsIntoSeatBrief(t *testing.T) {
	env := seatTestEnv(t, "")
	context := "The Task under review is Add the probe. Read its Brief and diff."
	c, err := Create(env, Opts{Question: "Review", Lanes: []string{"claude-sonnet"}, Context: context, Trigger: TriggerTask, Task: "abcd"})
	if err != nil {
		t.Fatal(err)
	}
	c, err = Load(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Context != context || !strings.Contains(councilBrief(c, c.Seats[0], 1, nil).Goal, context) {
		t.Fatalf("Task context lost: %+v", c)
	}
}
