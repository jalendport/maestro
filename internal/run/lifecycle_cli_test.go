package run_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/run"
)

// TestLifecycleHarness runs only when a fake Harness wrapper starts this binary.
func TestLifecycleHarness(t *testing.T) {
	kind := os.Getenv("MAESTRO_LIFECYCLE_HARNESS")
	if kind == "" {
		return
	}
	folder := os.Getenv("MAESTRO_TASK_FOLDER")
	if folder == "" {
		os.Exit(1)
	}
	runID := os.Getenv("MAESTRO_RUN")
	report := "## Answer\nThe fixture review is complete.\n\n## Findings\n- none\n\n## Concerns\n- none\n\nPOSITION: confirmed\n"
	if strings.HasSuffix(runID, "-plan") || strings.HasSuffix(runID, "-review") {
		marker := filepath.Join(os.Getenv("MAESTRO_LIFECYCLE_MARKERS"), runID)
		deadline := time.Now().Add(30 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(1)
			}
			time.Sleep(20 * time.Millisecond)
		}
	} else {
		name := os.Getenv("MAESTRO_TASK") + ".txt"
		if err := os.WriteFile(name, []byte("fixture change\n"), 0o644); err != nil {
			os.Exit(1)
		}
		report = "## Summary\nCreated the fixture file.\n\n## Changed\n- " + name + "\n\n## Checks\n- none\n\n## Blocked\n- none\n\n## Deviations\n- none\n\n## Open\n- none\n\nSTATUS: done\n"
	}
	if err := os.WriteFile(filepath.Join(folder, "result.md"), []byte(report), 0o644); err != nil {
		os.Exit(1)
	}
	if kind == "claude" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "result", "subtype": "success", "result": report})
	} else {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": report}})
		fmt.Println(`{"type":"turn.completed"}`)
	}
	os.Exit(0)
}

func TestCLIRunCouncilsSurviveAndCollectSeparately(t *testing.T) {
	fixture := t.TempDir()
	project := filepath.Join(fixture, "project")
	markers := filepath.Join(fixture, "markers")
	maestro := filepath.Join(fixture, "maestro")
	t.Setenv("MAESTRO_HOME", filepath.Join(fixture, "state"))
	t.Setenv("SOLO_PROCESS_ID", "")
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("MAESTRO_LIFECYCLE_MARKERS", markers)
	for _, dir := range []string{project, markers, filepath.Join(project, ".agents")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(fixture, "osascript"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$MAESTRO_LIFECYCLE_MARKERS/notification\"\n", 0o755)
	// On Linux the Claude pre-flight wants the sandbox tools on PATH.
	for _, name := range []string{"bwrap", "socat"} {
		write(filepath.Join(fixture, name), "#!/bin/sh\nexit 0\n", 0o755)
	}
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	build := exec.Command("go", "build", "-o", maestro, "../..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	git := exec.Command("git", "init", project)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("init fixture: %v\n%s", err, out)
	}
	write(filepath.Join(project, ".gitignore"), ".agents/\n", 0o644)
	write(config.ProjectFile(project), "router = \"rules\"\ncouncils = \"always\"\n", 0o644)
	if err := config.EnsureHome(); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	cache := &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{}}
	settings := "[router]\nbackend = \"rules\"\n[failure]\nkeep_awake = false\n"
	for _, kind := range []string{"claude", "codex", "grok", "agy"} {
		fake := filepath.Join(fixture, kind)
		write(fake, "#!/bin/sh\nMAESTRO_LIFECYCLE_HARNESS="+kind+" exec "+quote(self)+" -test.run=^TestLifecycleHarness$ -- \"$@\"\n", 0o755)
		settings += fmt.Sprintf("[harness.%s]\nbin = %q\n", kind, fake)
		if kind == "claude" {
			cache.Harness[kind] = discovery.Record{Path: fake, Version: "2.1.283", Models: []harness.Model{{ID: "opus"}, {ID: "sonnet"}}}
		} else if kind == "codex" {
			cache.Harness[kind] = discovery.Record{Path: fake, Version: "0.162.0", Models: []harness.Model{{ID: "gpt-6.1-sol"}}}
		}
	}
	write(config.UserFile(), settings, 0o644)
	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}
	bf := &budget.File{Accounts: map[string]*budget.Account{}}
	for _, account := range []string{"claude", "codex", "grok", "agy"} {
		bf.Record(account, []harness.Window{{Kind: "weekly", Used: .1, ResetsAt: time.Now().Add(time.Hour)}}, budget.SourceManual)
	}
	if err := bf.Save(); err != nil {
		t.Fatal(err)
	}
	plan := "---\naccess: edit\nlevel: none\ntouch: ['*.txt']\n---\n# Council lifecycle\n"
	for i := 1; i <= 2; i++ {
		plan += fmt.Sprintf("\n## Task %d\n- category: implement\n### Goal\nCreate the fixture file.\n### Done\nThe file exists.\n", i)
	}
	write(filepath.Join(project, "plan.md"), plan, 0o644)
	invokeAt := func(want int, root string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, maestro, args...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want {
			t.Fatalf("maestro %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	invoke := func(want int, args ...string) string {
		t.Helper()
		return invokeAt(want, project, args...)
	}
	invoke(2, "run", "plan.md", "--council", "--level", "significant", "--floor", "heavy", "--ceiling", "heavy")
	runFolder := filepath.Join(config.RunsDir(project), "r-0001")
	s, err := run.Load(runFolder)
	if err != nil || s.State != run.StateHeld || len(s.Tasks) != 2 {
		t.Fatalf("held Run = %+v, error = %v", s, err)
	}
	planSlug, reviewSlug := s.Run+"-plan", s.Run+"-review"
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(markers, planSlug), nil, 0o644)
		_ = os.WriteFile(filepath.Join(markers, reviewSlug), nil, 0o644)
		for _, slug := range []string{planSlug, reviewSlug} {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				c, err := council.Load(filepath.Join(config.CouncilsDir(project), slug))
				if err != nil || c.State == "finished" || !council.SupervisorAlive(c) {
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
	})
	planCouncilFolder := filepath.Join(config.CouncilsDir(project), planSlug)
	planCouncil, err := council.Load(planCouncilFolder)
	if err != nil || planCouncil.State == "finished" || !council.SupervisorAlive(planCouncil) {
		t.Fatalf("plan Council after CLI exit = %+v, error = %v", planCouncil, err)
	}
	write(filepath.Join(markers, planSlug), "", 0o644)
	invoke(0, "wait", planSlug)
	planCouncil, err = council.Load(planCouncilFolder)
	if err != nil || planCouncil.State != "finished" || planCouncil.SingleOpinion {
		t.Fatalf("collected plan Council = %+v, error = %v", planCouncil, err)
	}
	write(filepath.Join(project, "plan.md"), "# Invalid Plan\n", 0o644)
	if out := invoke(1, "approve", s.Run); !strings.Contains(out, "plan has no") {
		t.Fatalf("parse error not printed: %s", out)
	}
	stillHeld, err := run.Load(runFolder)
	if err != nil || stillHeld.State != run.StateHeld || len(stillHeld.Tasks) != 2 {
		t.Fatalf("Run after parse error = %+v, error = %v", stillHeld, err)
	}
	plan = strings.ReplaceAll(plan, "Council lifecycle", "Revised Plan")
	plan = strings.ReplaceAll(plan, "## Task", "## Revised Task")
	plan += "\n## New Task\n- category: implement\n- blocked_by: [1]\n### Goal\nCreate another fixture file.\n### Done\nThe file exists.\n"
	write(filepath.Join(project, "plan.md"), plan, 0o644)
	if out := invoke(0, "approve", s.Run); !strings.Contains(out, "3 tasks") || !strings.Contains(out, "New Task") || !strings.Contains(out, "provisional") {
		t.Fatalf("new provisional Routes not printed: %s", out)
	}
	invoke(0, "wait", s.Run)
	s, err = run.Load(runFolder)
	if err != nil || s.State != run.StateFinished || s.Run != "r-0001" || len(s.Tasks) != 3 || s.Title != "Revised Plan" || s.Tasks[2].Title != "New Task" || len(s.Tasks[2].BlockedBy) != 1 || s.Tasks[2].BlockedBy[0] != "Revised Task 1" {
		t.Fatalf("completed Run = %+v, error = %v", s, err)
	}
	planCouncils := 0
	for _, slug := range s.Councils {
		if strings.Contains(slug, "-plan") {
			planCouncils++
		}
	}
	if planCouncils != 1 {
		t.Fatalf("plan Councils after approval = %v", s.Councils)
	}
	for _, task := range s.Tasks {
		dispatched, err := dispatch.LoadTask(task.Folder)
		if err != nil || dispatched.Route.Level != roster.LevelSignificant || dispatched.Route.Tier != roster.TierHeavy {
			t.Fatalf("dispatch route = %+v, error = %v", dispatched, err)
		}
	}
	if s.Level != roster.LevelSignificant || s.Floor != roster.TierHeavy || s.Ceiling != roster.TierHeavy {
		t.Fatalf("saved route flags = level %s floor %s ceiling %s", s.Level, s.Floor, s.Ceiling)
	}
	report, err := os.ReadFile(filepath.Join(runFolder, run.FileReport))
	if err != nil || !strings.Contains(string(report), reviewSlug) {
		t.Fatalf("Run report = %s, error = %v", report, err)
	}
	notification, err := os.ReadFile(filepath.Join(markers, "notification"))
	if err != nil || !strings.Contains(string(notification), reviewSlug) {
		t.Fatalf("Run notification = %s, error = %v", notification, err)
	}
	rows, err := os.ReadFile(config.LedgerFile())
	if err != nil || !strings.Contains(string(rows), `"kind":"run"`) {
		t.Fatalf("Run ledger row absent, error = %v", err)
	}
	reviewFolder := filepath.Join(config.CouncilsDir(project), reviewSlug)
	review, err := council.Load(reviewFolder)
	if err != nil || review.State == "finished" {
		t.Fatalf("end Council should still be pending: %+v, error = %v", review, err)
	}
	write(filepath.Join(markers, reviewSlug), "", 0o644)
	invoke(0, "wait", reviewSlug)
	review, err = council.Load(reviewFolder)
	if err != nil || review.State != "finished" || review.SingleOpinion {
		t.Fatalf("collected end Council = %+v, error = %v", review, err)
	}

	for _, tc := range []struct {
		name, config, response string
		tasks                  int
		flags                  []string
		council                bool
	}{
		{name: "six Tasks", tasks: 6},
		{name: "explicit Council", tasks: 6, flags: []string{"--council"}, council: true},
		{name: "size disabled by flag", tasks: 12, flags: []string{"--no-council"}},
		{name: "no Council flag wins", tasks: 12, flags: []string{"--council", "--no-council"}},
		{name: "default threshold", tasks: 10, council: true},
		{name: "custom threshold", tasks: 6, config: "[run]\ncouncil_tasks = 6\n", council: true},
		{name: "below custom threshold", tasks: 6, config: "[run]\ncouncil_tasks = 7\n"},
		{name: "size rule off", tasks: 12, config: "[run]\ncouncil_tasks = 0\n"},
		{name: "Router question", tasks: 1, response: `"lane":{"choice":"ask_orchestrator","confidence":0.95},"spec_is_concrete":{"noul":1}`, council: true},
		{name: "Router question disabled", tasks: 1, flags: []string{"--no-council"}, response: `"lane":{"choice":"ask_orchestrator","confidence":0.95},"spec_is_concrete":{"noul":1}`},
		{name: "Router Council signal", tasks: 1, response: `"spec_is_concrete":{"noul":1},"wants_independent_view":{"noul":1},"needs_write":{"noul":0}`, council: true},
		{name: "Router Council signal disabled", tasks: 1, flags: []string{"--no-council"}, response: `"spec_is_concrete":{"noul":1},"wants_independent_view":{"noul":1},"needs_write":{"noul":0}`},
		{name: "fallback Route", tasks: 1, response: `"spec_is_concrete":{"noul":1}`},
		{name: "medium Route", tasks: 1, response: `"lane":{"choice":"codex-sol","confidence":0.5},"spec_is_concrete":{"noul":1},"judgment_level":{"score":1}`},
		{name: "level gap", tasks: 1, response: `"lane":{"choice":"codex-sol","confidence":0.95},"spec_is_concrete":{"noul":1},"judgment_level":{"score":3}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o755); err != nil {
				t.Fatal(err)
			}
			write(config.ProjectFile(root), "", 0o644)
			user := settings + tc.config
			if tc.response != "" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					fmt.Fprintf(w, `{"answers":{%s}}`, tc.response)
				}))
				defer server.Close()
				user = strings.Replace(user, "backend = \"rules\"", "backend = \"jev\"\nbase_url = "+fmt.Sprintf("%q", server.URL), 1)
				t.Setenv("TYPESAFE_API_KEY", "fixture-key")
			}
			write(config.UserFile(), user, 0o644)
			src := "---\naccess: edit\nlevel: some\ntouch: ['*.txt']\n---\n# Approval rules\n"
			for i := 1; i <= tc.tasks; i++ {
				src += fmt.Sprintf("\n## Task %d\n### Goal\nCreate the fixture file.\n### Done\nThe file exists.\n", i)
			}
			write(filepath.Join(root, "plan.md"), src, 0o644)
			args := append([]string{"run", "plan.md"}, tc.flags...)
			out := invokeAt(2, root, args...)
			wantHeld := "run r-0001 held.  Fold any findings into the Plan file, then: maestro approve r-0001"
			if !strings.Contains(out, wantHeld) {
				t.Fatalf("held message = %s", out)
			}
			held, err := run.Load(filepath.Join(config.RunsDir(root), "r-0001"))
			if err != nil || held.State != run.StateHeld || len(held.Tasks) != tc.tasks {
				t.Fatalf("held Run = %+v, error = %v", held, err)
			}
			if got := len(held.Councils) > 0; got != tc.council {
				t.Fatalf("plan Councils = %v, want Council %v\n%s", held.Councils, tc.council, out)
			}
			status := invokeAt(0, root, "status", held.Run)
			if !strings.Contains(status, "held") || (tc.name == "six Tasks" && strings.Contains(strings.ToLower(status), "council")) {
				t.Fatalf("status = %s", status)
			}
			if tc.council {
				invokeAt(0, root, "wait", held.Councils[0])
			} else if entries, err := os.ReadDir(config.CouncilsDir(root)); err == nil && len(entries) != 0 {
				t.Fatalf("unexpected Council folders: %v", entries)
			}
		})
	}
}
