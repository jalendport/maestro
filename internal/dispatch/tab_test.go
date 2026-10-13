package dispatch

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/solo"
)

func TestSoloTabHelper(t *testing.T) {
	if os.Getenv("MAESTRO_TEST_SOLO_HELPER") != "1" {
		return
	}
	calls, err := os.Create(os.Getenv("MAESTRO_TEST_CALLS"))
	if err != nil {
		panic(err)
	}
	defer calls.Close()
	statusCalls := 0
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					TodoID int `json:"todo_id"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			panic(err)
		}
		if req.ID == 0 {
			continue
		}
		var result any = map[string]any{}
		if req.Method == "tools/call" {
			if _, err := calls.Write(append(sc.Bytes(), '\n')); err != nil {
				panic(err)
			}
			text := `{}`
			switch req.Params.Name {
			case "todo_get":
				if req.Params.Arguments.TodoID == 999 {
					result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "todo not found"}}}
					if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
						panic(err)
					}
					continue
				}
				text = fmt.Sprintf(`{"todo":{"id":%d}}`, req.Params.Arguments.TodoID)
			case "todo_create":
				text = `{"todo_id":7}`
			case "spawn_agent":
				if os.Getenv("MAESTRO_TEST_TAB_LIMIT") == "" && os.Getenv("MAESTRO_TEST_TAB_NO_REPORT") == "" {
					if err := os.WriteFile(os.Getenv("MAESTRO_TEST_REPORT"), []byte("## Answer\nPASS\n\n## Findings\n- none\n\n## Concerns\n- none\n\nPOSITION: confirmed\n"), 0o644); err != nil {
						panic(err)
					}
				}
				text = `{"process_id":42}`
			case "get_process_status":
				statusCalls++
				text = `{"status":"Exited","uptime_seconds":100}`
				if os.Getenv("MAESTRO_TEST_TAB_LIMIT") != "" {
					text = `{"status":"Exited","uptime_seconds":1}`
				}
				switch os.Getenv("MAESTRO_TEST_TAB_NO_REPORT") {
				case "unchanged":
					if statusCalls == 1 {
						text = `{"status":"Running","uptime_seconds":100}`
					}
				case "worked", "blank then prompt":
					if statusCalls <= 2 {
						text = `{"status":"Running","uptime_seconds":100}`
					}
				case "wall", "worked wall":
					text = `{"status":"Running","uptime_seconds":100}`
				}
			case "get_process_output":
				text = `{"output":"Council Seat finished"}`
				if os.Getenv("MAESTRO_TEST_TAB_LIMIT") != "" {
					text = `{"output":"Quota exceeded"}`
				}
				if mode := os.Getenv("MAESTRO_TEST_TAB_NO_REPORT"); mode != "" {
					text = `{"output":"Trust this folder?"}`
					if mode == "blank then prompt" && statusCalls == 1 {
						text = `{"output":""}`
					}
					if strings.HasPrefix(mode, "worked") && statusCalls >= 2 {
						text = `{"output":"The Lane read the Brief and started work."}`
					}
				}
			}
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			panic(err)
		}
	}
	os.Exit(0)
}

func TestTabNoReportStartupIsNotRetried(t *testing.T) {
	for _, mode := range []string{"unchanged", "blank then prompt", "wall", "worked", "worked wall"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("MAESTRO_TEST_TAB_NO_REPORT", mode)
			client, callsPath := deferredSoloClient(t)
			root, folder := t.TempDir(), t.TempDir()
			r := &roster.Roster{Harness: map[string]config.Harness{"codex": {SoloTool: 9}}}
			off := false
			r.Failure.KeepAwake = &off
			r.Concurrency.CloseTabs = "never"
			env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": "/must-not-run"}), Solo: &SoloLink{Client: client, ProjectID: 1}}
			b := &brief.Brief{Fields: brief.Fields{Title: "Startup", Access: brief.AccessRead}, Goal: "Read", Done: "Report", Path: filepath.Join(root, "source.md")}
			if err := brief.Write(b, b.Path); err != nil {
				t.Fatal(err)
			}
			task := &Task{Project: root, Folder: folder, Task: "abcd", Access: brief.AccessRead,
				Route: &router.Route{Harness: "codex", FamilyID: "codex-sol", Family: &roster.Family{ID: "codex-sol"}}, BriefPath: b.Path, Council: true, MaxAttempt: 2, WallLimit: "1m", IdleLimit: "1m"}
			if mode == "wall" {
				task.WallLimit = time.Nanosecond.String()
			}
			if mode == "worked wall" {
				task.WallLimit = "1s"
			}
			var rec *Record
			var err error
			if strings.HasPrefix(mode, "worked") {
				rec = runOnce(env, task, b, 1, nil)
			} else {
				rec, err = Execute(env, task)
			}
			if err != nil {
				t.Fatal(err)
			}
			wantClass, wantText := FailLane, tabStartupError
			if mode == "worked" {
				wantClass, wantText = FailTransient, "the tab ended with no report"
			}
			if mode == "worked wall" {
				wantClass, wantText = FailStall, ""
			}
			if rec.Outcome.FailureClass != wantClass || rec.Outcome.ErrorText != wantText {
				t.Fatalf("outcome: %+v", rec.Outcome)
			}
			_, retry := retryWait(rec.Outcome)
			if retry != (mode == "worked") {
				t.Fatalf("retry=%v for %+v", retry, rec.Outcome)
			}
			calls, err := os.ReadFile(callsPath)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(calls), `"name":"spawn_agent"`); got != 1 {
				t.Fatalf("spawn count=%d, want 1", got)
			}
			log, err := os.ReadFile(filepath.Join(folder, "tab.log"))
			if err != nil || len(log) == 0 {
				t.Fatalf("tab.log=%q, err=%v", log, err)
			}
		})
	}
}

func TestTabOutputFirstScreenIsNotWork(t *testing.T) {
	o := &tabOutput{}
	for _, text := range []string{"", "  ", "Trust this folder?", "Trust this folder?"} {
		o.observe(text)
		if o.worked {
			t.Fatalf("first screen counted as work: %q", text)
		}
	}
	if !o.observe("Reading the Brief") || !o.worked {
		t.Fatal("output change did not count as work")
	}
}

func TestCouncilSeatUsesCodexReadTabAndCloseTabs(t *testing.T) {
	for _, closeTabs := range []string{"always", "never"} {
		t.Run(closeTabs, func(t *testing.T) {
			root := t.TempDir()
			if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, out)
			}
			folder := t.TempDir()
			t.Setenv("CODEX_HOME", t.TempDir())
			t.Setenv("MAESTRO_TEST_SOLO_HELPER", "1")
			t.Setenv("MAESTRO_TEST_REPORT", filepath.Join(folder, FileReport))
			callsPath := filepath.Join(t.TempDir(), "calls.jsonl")
			t.Setenv("MAESTRO_TEST_CALLS", callsPath)
			bin, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("MAESTRO_TEST_BINARY", bin)
			helper := filepath.Join(t.TempDir(), "solo-helper")
			if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MAESTRO_TEST_BINARY\" -test.run=TestSoloTabHelper\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MAESTRO_SOLO_MCP", helper)
			client, err := solo.Connect()
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			r := &roster.Roster{Harness: map[string]config.Harness{"codex": {SoloTool: 9}}}
			r.Concurrency.CloseTabs = closeTabs
			link := &SoloLink{Client: client, ProjectID: 1}
			env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": "/must-not-run-one-shot"}), Solo: link}
			task := &Task{Project: root, Folder: folder, Run: "council", Task: "seat", Access: brief.AccessRead, Council: true,
				Route: &router.Route{Harness: "codex", Model: "test-model", Effort: "high"}, WallLimit: "1m", IdleLimit: "1m"}
			rec := runOnce(env, task, &brief.Brief{Title: "Council", Files: []string{"main.go"}}, 1, nil)
			if rec.SoloProcess != 42 || rec.Outcome.Outcome != OutcomeAnswered || rec.Outcome.ReportByText {
				t.Fatalf("Council tab outcome: %+v", rec)
			}
			link.CloseTab(env, rec)
			calls, err := os.ReadFile(callsPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), `"name":"send_input"`) {
				t.Fatal("Pointer was typed after launch")
			}
			closed := strings.Contains(string(calls), `"name":"close_process"`)
			if closed != (closeTabs == "always") {
				t.Fatalf("close_tabs %s: calls %s", closeTabs, calls)
			}
			var spawn struct {
				Params struct {
					Arguments struct {
						ExtraArgs []string `json:"extra_args"`
					} `json:"arguments"`
				} `json:"params"`
			}
			if err := json.Unmarshal([]byte(strings.Split(string(calls), "\n")[0]), &spawn); err != nil {
				t.Fatal(err)
			}
			args := spawn.Params.Arguments.ExtraArgs
			if len(args) < 6 || !strings.Contains(args[0], root) || !strings.Contains(args[0], filepath.Join(folder, FileBrief)) || args[3] != "-C" || args[4] != folder {
				t.Fatalf("Codex read tab launch: %q", args)
			}
			rendered, err := os.ReadFile(filepath.Join(folder, FileBrief))
			if err != nil || !strings.Contains(string(rendered), filepath.Join(root, "main.go")) || !strings.Contains(string(rendered), filepath.Join(folder, FileReport)) || strings.Contains(string(rendered), "Do not try to write a file") {
				t.Fatalf("read tab Brief: %s, %v", rendered, err)
			}
			log, err := os.ReadFile(filepath.Join(folder, "tab.log"))
			if err != nil || string(log) != "Council Seat finished" {
				t.Fatalf("tab.log: %q, %v", log, err)
			}
		})
	}
}

func TestMissingSoloPresetFallsBackWithoutChangingEnv(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	folder := t.TempDir()
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '## Answer\\nPASS\\n\\n## Findings\\n- none\\n\\n## Concerns\\n- none\\n\\nSTATUS: done\\n' > \"$MAESTRO_TEST_REPORT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_REPORT", filepath.Join(folder, FileReport))
	link := &SoloLink{}
	env := &Env{Root: root, Roster: &roster.Roster{}, Reg: harness.New(map[string]string{"codex": bin}), Solo: link}
	task := &Task{Project: root, Folder: folder, Run: "run", Task: "task", Access: brief.AccessRead,
		Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{Title: "Read task"}, 1, nil)
	if rec.SoloProcess != 0 || rec.Outcome.Outcome != OutcomeAnswered || !rec.Outcome.ReportByText || env.Solo != link {
		t.Fatalf("one-shot fallback: %+v", rec)
	}
}

func TestTabLimitBeforeEarlyExitIsClassified(t *testing.T) {
	root, folder := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("MAESTRO_TEST_SOLO_HELPER", "1")
	t.Setenv("MAESTRO_TEST_TAB_LIMIT", "1")
	t.Setenv("MAESTRO_TEST_CALLS", filepath.Join(t.TempDir(), "calls"))
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_TEST_BINARY", bin)
	helper := filepath.Join(t.TempDir(), "solo-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexec \"$MAESTRO_TEST_BINARY\" -test.run=TestSoloTabHelper\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAESTRO_SOLO_MCP", helper)
	client, err := solo.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	r := &roster.Roster{Harness: map[string]config.Harness{"codex": {SoloTool: 9}}}
	env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": "/must-not-run"}), Solo: &SoloLink{Client: client, ProjectID: 1}}
	task := &Task{Project: root, Folder: folder, Task: "abcd", Access: brief.AccessRead, Route: &router.Route{Harness: "codex"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.FailureClass != FailLimit || rec.Outcome.ErrorText != "Quota exceeded" {
		t.Fatalf("outcome: %+v", rec.Outcome)
	}
}
