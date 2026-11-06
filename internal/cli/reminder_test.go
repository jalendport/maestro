package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/council"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/run"
)

func reminderJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func liveReminderFixture(t *testing.T) (string, *run.State, *run.State, string) {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GROK_HOME", t.TempDir())
	t.Setenv("MAESTRO_LANE", "")
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	// The Reminder must work without a Harness, Solo helper, or network client.
	t.Setenv("PATH", "")
	if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \"projects\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	reminderJSON(t, config.ProjectFile(root), map[string]any{})
	paused := &run.State{
		Run: "r-0001", Folder: filepath.Join(config.RunsDir(root), "r-0001"),
		State: run.StatePaused, PauseReason: "same error twice\ncheck the Brief",
		Tasks: []*run.TaskState{{Number: 1, ID: "aaaa", State: run.TaskBlocked}},
	}
	reminderJSON(t, filepath.Join(paused.Folder, run.FileState), paused)
	live := &run.State{
		Run: "r-0002", Folder: filepath.Join(config.RunsDir(root), "r-0002"), State: run.StateLive, Pid: os.Getpid(),
		Tasks: []*run.TaskState{
			{Number: 2, ID: "bbbb", State: run.TaskBlocked, Error: "needs a corrected Brief"},
			{Number: 3, ID: "cccc", Title: "Read the project", State: run.TaskRunning},
			{Number: 4, ID: "dddd", State: run.TaskBlocked, DependencyBlocked: true},
		},
	}
	reminderJSON(t, filepath.Join(live.Folder, run.FileState), live)
	folder := filepath.Join(live.Folder, "cccc")
	rec := &dispatch.Record{
		Task: "cccc", State: "running", Path: folder, Pid: os.Getpid(), StartedAt: time.Now().Add(-2 * time.Minute),
		TaskInfo: dispatch.TaskInfo{Title: "Read the project"}, Route: &router.Route{FamilyID: "codex-sol", Effort: "high"},
	}
	reminderJSON(t, filepath.Join(folder, dispatch.FileRecord), rec)
	control, err := config.ControlDirForFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	reminderJSON(t, filepath.Join(control, "relay.json"), dispatch.RelayRequest{
		ID: "current", ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"make deploy"}`), AskedAt: time.Now(),
	})
	reminderJSON(t, filepath.Join(control, "relay.lock"), map[string]any{"id": "current", "pid": os.Getpid(), "at": time.Now()})
	return root, paused, live, control
}

func reminderPayload(t *testing.T, harness, cwd string) string {
	t.Helper()
	payload := map[string]any{"hook_event_name": "UserPromptSubmit", "cwd": cwd, "prompt": "Continue."}
	if harness == "agy" {
		payload = map[string]any{"invocationNum": 1, "workspacePaths": []string{cwd}}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func reminderText(t *testing.T, harness, output string) string {
	t.Helper()
	var body struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
		InjectSteps []struct {
			EphemeralMessage string `json:"ephemeralMessage"`
		} `json:"injectSteps"`
	}
	if err := json.Unmarshal([]byte(output), &body); err != nil {
		t.Fatal(err)
	}
	if harness == "agy" {
		if len(body.InjectSteps) != 1 {
			t.Fatalf("injectSteps = %+v", body.InjectSteps)
		}
		return body.InjectSteps[0].EphemeralMessage
	}
	if body.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Fatalf("wrong Reminder event: %s", output)
	}
	return body.HookSpecificOutput.AdditionalContext
}

func requireLiveReminder(t *testing.T, text string) {
	t.Helper()
	lines := strings.Split(text, "\n")
	if len(lines) != 5 || lines[0] != adapter.Reminder {
		t.Fatalf("want fixed text and four live items, got %q", text)
	}
	for _, want := range []string{
		`relay  task cccc  Bash {"command":"make deploy"}  maestro answer cccc allow|deny --relay current`,
		"paused  run r-0001  same error twice check the Brief  maestro resume r-0001 --task 1 (--lane <family[:effort]> | --brief <file> | --skip)",
		"blocked  run r-0002  task bbbb  needs a corrected Brief  maestro resume r-0002 --task 2 (--lane <family[:effort]> | --brief <file> | --skip)",
		"running  task cccc  Read the project  codex-sol high  2m",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Reminder lacks %q: %s", want, text)
		}
	}
}

func TestHookReminderLiveItems(t *testing.T) {
	root, _, _, _ := liveReminderFixture(t)
	// Prompts in a child folder use the Project file's root.
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, harness := range []string{"claude", "codex", "agy"} {
		t.Run(harness, func(t *testing.T) {
			output := runHookPayload(t, reminderPayload(t, harness, nested), harness)
			requireLiveReminder(t, reminderText(t, harness, output))
		})
	}
	// Another project must not inherit items from the global control folder.
	other := t.TempDir()
	reminderJSON(t, config.ProjectFile(other), map[string]any{})
	output := runHookPayload(t, reminderPayload(t, "codex", other), "codex")
	if got := reminderText(t, "codex", output); got != adapter.Reminder {
		t.Fatalf("other project = %q", got)
	}
}

func TestHookReminderNoLiveItems(t *testing.T) {
	root, paused, live, _ := liveReminderFixture(t)
	paused.State, live.State = run.StateFinished, run.StateCancelled
	reminderJSON(t, filepath.Join(paused.Folder, run.FileState), paused)
	reminderJSON(t, filepath.Join(live.Folder, run.FileState), live)
	reminderJSON(t, filepath.Join(config.RunsDir(root), "r-0003", "ended", dispatch.FileRecord), &dispatch.Record{
		Task: "ended", State: "ended", Events: []dispatch.Event{{Kind: "relay", Text: "old Bash make deploy"}},
	})
	for _, harness := range []string{"claude", "codex", "agy"} {
		output := runHookPayload(t, reminderPayload(t, harness, root), harness)
		if got := reminderText(t, harness, output); got != adapter.Reminder {
			t.Fatalf("%s: Reminder = %q", harness, got)
		}
	}
}

func TestHookReminderResolvedItems(t *testing.T) {
	root, paused, live, control := liveReminderFixture(t)
	if err := dispatch.WriteRelayAnswer(control, "deny", "current"); err != nil {
		t.Fatal(err)
	}
	reminderJSON(t, filepath.Join(paused.Folder, "control", "resume-aaaa-1.json"), run.Resume{Task: "aaaa", Skip: true})
	reminderJSON(t, filepath.Join(live.Folder, "control", "resume-bbbb-1.json"), run.Resume{Task: "bbbb", Skip: true})
	output := runHookPayload(t, reminderPayload(t, "codex", root), "codex")
	text := reminderText(t, "codex", output)
	if lines := strings.Split(text, "\n"); len(lines) != 2 || lines[0] != adapter.Reminder || !strings.HasPrefix(lines[1], "running  task cccc ") {
		t.Fatalf("resolved items still shown: %q", text)
	}
	b, err := os.ReadFile(filepath.Join(control, "answer-deny"))
	if err != nil || string(b) != "current" {
		t.Fatalf("hook consumed the Relay answer: %q, %v", b, err)
	}
}

func TestHookReminderStandaloneDispatch(t *testing.T) {
	root, _, live, _ := liveReminderFixture(t)
	// Removing run.json makes this a single Dispatch with the same running Lane.
	if err := os.Remove(filepath.Join(live.Folder, run.FileState)); err != nil {
		t.Fatal(err)
	}
	output := runHookPayload(t, reminderPayload(t, "codex", root), "codex")
	text := reminderText(t, "codex", output)
	if strings.Count(text, "\n") != 3 || !strings.Contains(text, "relay  task cccc") || !strings.Contains(text, "codex-sol high  2m") {
		t.Fatalf("single Dispatch missing: %q", text)
	}
}

func TestHookReminder200RunFolders(t *testing.T) {
	root, _, _, _ := liveReminderFixture(t)
	for n := 3; n <= 200; n++ {
		folder := filepath.Join(config.RunsDir(root), fmt.Sprintf("r-%04d", n))
		reminderJSON(t, filepath.Join(folder, run.FileState), &run.State{Run: filepath.Base(folder), State: run.StateFinished})
		reminderJSON(t, filepath.Join(folder, "old", dispatch.FileRecord), &dispatch.Record{State: "ended"})
	}
	payload := reminderPayload(t, "codex", root)
	start := time.Now()
	output := runHookPayload(t, payload, "codex")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Reminder for 200 Run folders took %s", elapsed)
	}
	requireLiveReminder(t, reminderText(t, "codex", output))
}

func TestHookReminderCouncilLanes(t *testing.T) {
	root, _, _, _ := liveReminderFixture(t)
	folder := filepath.Join(config.CouncilsDir(root), "review")
	reminderJSON(t, filepath.Join(folder, council.FileManifest), &council.Council{
		Slug: "review", Path: folder, State: "running", Pid: os.Getpid(), RoundsRun: 2,
		Seats: []*council.Seat{{Family: "claude-fable", Effort: "high"}},
	})
	for round, path := range map[int]string{1: folder, 2: filepath.Join(folder, "round-2")} {
		reminderJSON(t, filepath.Join(path, "claude-fable", dispatch.FileRecord), &dispatch.Record{
			Task: fmt.Sprintf("review-round-%d", round), State: "running", Pid: os.Getpid(), StartedAt: time.Now().Add(-time.Minute),
			Route: &router.Route{FamilyID: "claude-fable", Effort: "high"},
		})
	}
	output := runHookPayload(t, reminderPayload(t, "codex", root), "codex")
	text := reminderText(t, "codex", output)
	if strings.Contains(text, "review-round-1") || !strings.Contains(text, "running  task review-round-2") || !strings.Contains(text, "claude-fable high  1m") {
		t.Fatalf("Council Lanes = %q", text)
	}
}
