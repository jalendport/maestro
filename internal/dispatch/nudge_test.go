package dispatch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
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
