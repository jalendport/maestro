package dispatch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

func TestDoneItemsKeepContinuationAndNestedLists(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"", nil},
		{"A plain Done item.", []string{"A plain Done item."}},
		{"- First item\n  continued\n  - nested detail\n- Second item", []string{"First item\n  continued\n  - nested detail", "Second item"}},
		{"1. First item\n2. Second item", []string{"First item", "Second item"}},
		{"  * First item\n  * Second item", []string{"First item", "Second item"}},
	} {
		if got := doneItems(tc.text); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Done=%q items=%q want=%q", tc.text, got, tc.want)
		}
	}
}

func TestPendingNudgeDoesNotPublishAnEndedTask(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"answers":{"satisfies":{"noul":0.55},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}},"done_1":{"noul":0.2}}}`)
	}))
	defer server.Close()
	root, folder := causeRoot(t), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "work.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	r.Router.Backend, r.Router.BaseURL = "jev", server.URL
	env := &Env{Roster: r}
	rec := &Record{Path: folder, State: "running", StartedAt: time.Now()}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	judge(env, &Task{Project: root, Access: brief.AccessEdit}, rec, brief.ParseReport("STATUS: done"), []string{"true"}, &brief.Brief{Done: "- The behavior works."}, attemptBaseline{})
	if !rec.nudgePending || rec.State != "ended" {
		t.Fatalf("nudge was not queued: %+v", rec)
	}
	saved, err := LoadRecord(folder)
	if err != nil || saved.State != "running" {
		t.Fatalf("wait can stop before the nudge: Record=%+v error=%v", saved, err)
	}
}

func TestNewRouteKeepsTheNudgeMark(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := causeRoot(t)
	env := &Env{Root: root, Roster: &roster.Roster{}}
	folder := filepath.Join(config.RunsDir(root), "r-test", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (&Task{Folder: folder, Nudged: true}).Save(); err != nil {
		t.Fatal(err)
	}
	b := &brief.Brief{Fields: brief.Fields{Access: brief.AccessEdit}, Done: "- The behavior works."}
	task, err := PrepareRouted(env, b, &router.Route{}, "r-test", "abcd", "")
	if err != nil || !task.Nudged {
		t.Fatalf("new Route forgot the nudge: Task=%+v error=%v", task, err)
	}
	gate, _ := json.Marshal(Gate{Satisfies: 0.55, Verdict: "pass", Risk: "low", DoneItems: map[int]float64{1: 0.2}})
	rec := &Record{Outcome: Outcome{Outcome: OutcomeUnverified}, Verification: Verification{Gate: gate, Checks: []verify.CheckResult{{OK: true}}}}
	if findings := nudgeFindings(env, task, rec, b); len(findings) > 0 {
		t.Fatalf("new Route got a second nudge: %v", findings)
	}
}

func TestNudgeRunsOnceWithoutChecks(t *testing.T) {
	root, folder := causeRoot(t), t.TempDir()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	r, err := roster.Load()
	if err != nil {
		t.Fatal(err)
	}
	off := false
	r.Failure.KeepAwake = &off
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"answers":{"satisfies":{"noul":0.55},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}},"done_1":{"noul":0.2}}}`)
	}))
	defer server.Close()
	r.Router.Backend, r.Router.BaseURL = "jev", server.URL
	bin := filepath.Join(t.TempDir(), "lane")
	script := "#!/bin/sh\nread pointer\nprintf 'work\\n' > \"$TEST_NUDGE_WORK\"\nprintf 'STATUS: done\\n' > \"$TEST_NUDGE_REPORT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_NUDGE_WORK", filepath.Join(root, "work.txt"))
	t.Setenv("TEST_NUDGE_REPORT", filepath.Join(folder, FileReport))
	b := &brief.Brief{Fields: brief.Fields{Title: "Complete the work", Access: brief.AccessEdit, Touch: []string{"work.txt"}}, Goal: "Complete the work.", Done: "- The behavior works."}
	b.Path = filepath.Join(root, "source.md")
	if err := brief.Write(b, b.Path); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: r, Reg: harness.New(map[string]string{"codex": bin})}
	task := &Task{Project: root, Folder: folder, BriefPath: b.Path, Access: brief.AccessEdit,
		Route:      &router.Route{Family: r.Families["codex-sol"], FamilyID: "codex-sol", Harness: "codex"},
		MaxAttempt: 1, IdleLimit: "1m", WallLimit: "1m"}
	rec, err := Execute(env, task)
	if err != nil || rec.Attempt != 2 || !rec.Nudged || calls != 2 || rec.Outcome.Outcome != OutcomeUnverified || len(rec.Verification.Checks) != 0 {
		t.Fatalf("record=%+v calls=%d error=%v", rec, calls, err)
	}
	text, err := os.ReadFile(filepath.Join(folder, FileBrief))
	if err != nil || !strings.Contains(string(text), "Keep that work") || strings.Contains(string(text), "Confirmed findings") || strings.Contains(string(text), "discard") {
		t.Fatalf("nudge Brief: %s error=%v", text, err)
	}
}
