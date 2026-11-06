package dispatch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/jev"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

type causeRequest struct {
	State struct {
		Brief                 struct{ Title, Goal, Done string }
		Blocked, Status, Diff string
		FailedChecks          string `json:"failed_checks"`
	}
	Questions map[string]jev.Question
}

func causeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return root
}

func TestBadResultAsksCauseAlone(t *testing.T) {
	for _, tc := range []struct {
		name, text, check, cause string
	}{
		{"failed check", "STATUS: done", "printf 'missing module\\n'; false", CauseEnvironment},
		{"blocked Report", "## Blocked\n- required.go is outside touch\n\nSTATUS: blocked", "", CauseBrief},
		{"partial empty diff", "## Blocked\n- sandbox denied the tool\n\nSTATUS: partial", "", CauseEnvironment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("TYPESAFE_API_KEY", "test-key")
			var requests []causeRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request causeRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				requests = append(requests, request)
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"cause": map[string]string{"choice": tc.cause}}})
			}))
			defer server.Close()
			root, folder := causeRoot(t), t.TempDir()
			env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}}
			rec := &Record{Path: folder}
			task := &Task{Project: root, Title: "Task title", Access: brief.AccessEdit}
			report := brief.ParseReport(tc.text)
			var checks []string
			if tc.check != "" {
				checks = []string{tc.check}
			}
			judge(env, task, rec, report, checks, &brief.Brief{Goal: "Task goal", Done: "Task Done"}, attemptBaseline{})
			if len(requests) != 1 || len(requests[0].Questions) != 1 || requests[0].Questions["cause"].Type != "choice" {
				t.Fatalf("cause requests: %+v", requests)
			}
			state := requests[0].State
			if state.Brief.Title != task.Title || state.Brief.Goal != "Task goal" || state.Brief.Done != "Task Done" || state.Status != report.Status || state.Blocked != report.Sections["Blocked"] {
				t.Fatalf("state: %+v", state)
			}
			if tc.check != "" && !strings.Contains(state.FailedChecks, "missing module\n") {
				t.Fatalf("check output missing: %+v", state)
			}
			var gate Gate
			if err := json.Unmarshal(rec.Verification.Gate, &gate); err != nil || gate.Cause != tc.cause || gate.CauseSource != "jev" {
				t.Fatalf("gate: %+v, %v", gate, err)
			}
			saved, err := LoadRecord(folder)
			if err != nil || saved.ResolveCause() != tc.cause {
				t.Fatalf("saved Cause: %+v, %v", saved, err)
			}
		})
	}
}

func TestTouchViolationHasBriefCauseWithoutJev(t *testing.T) {
	for _, status := range []string{brief.StatusDone, brief.StatusPartial, brief.StatusBlocked} {
		t.Run(status, func(t *testing.T) {
			t.Setenv("TYPESAFE_API_KEY", "test-key")
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.WriteString(w, `{"answers":{"cause":{"choice":"capability"}}}`)
			}))
			defer server.Close()
			root, folder := causeRoot(t), t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "forbidden.txt"), []byte("change"), 0o644); err != nil {
				t.Fatal(err)
			}
			env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}}
			rec := &Record{Path: folder}
			judge(env, &Task{Project: root, Access: brief.AccessEdit, Touch: []string{"allowed.txt"}}, rec, brief.ParseReport("STATUS: "+status), nil, &brief.Brief{}, attemptBaseline{})
			var gate Gate
			if err := json.Unmarshal(rec.Verification.Gate, &gate); err != nil || gate.Cause != CauseBrief || gate.CauseSource != "rule" || calls != 0 {
				t.Fatalf("gate=%+v calls=%d error=%v", gate, calls, err)
			}
		})
	}
}

func TestGateAsksCauseWithVerification(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	var requests []causeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request causeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		requests = append(requests, request)
		_, _ = io.WriteString(w, `{"answers":{"cause":{"choice":"capability"},"verdict":{"choice":"wrong_logic"}}}`)
	}))
	defer server.Close()
	root, folder := causeRoot(t), t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "work.txt"), []byte("wrong work"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}}
	rec := &Record{Path: folder}
	judge(env, &Task{Project: root, Access: brief.AccessEdit}, rec, brief.ParseReport("STATUS: done"), nil, &brief.Brief{}, attemptBaseline{})
	if len(requests) != 1 || len(requests[0].Questions) != 4 || requests[0].State.Diff == "" || rec.Outcome.FailureClass != FailBad || rec.ResolveCause() != CauseCapability {
		t.Fatalf("requests=%+v record=%+v", requests, rec)
	}
}

func TestCauseFallbackWhenJevIsAbsent(t *testing.T) {
	for _, backend := range []string{"rules", "project rules", "no key", "error", "invalid answer"} {
		t.Run(backend, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("TYPESAFE_API_KEY", "test-key")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if backend == "invalid answer" {
					_, _ = io.WriteString(w, `{"answers":{"cause":{"choice":"unknown"}}}`)
				} else {
					http.Error(w, "unavailable", http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}}
			switch backend {
			case "rules":
				env.Roster.Router.Backend = "rules"
			case "project rules":
				env.Project = &config.Project{Router: "rules"}
			case "no key":
				t.Setenv("TYPESAFE_API_KEY", "")
			}
			for _, tc := range []struct{ report, cause string }{
				{"## Blocked\n- required file is excluded\nSTATUS: blocked", CauseBrief},
				{"## Blocked\n- tool is missing\nSTATUS: partial", CauseEnvironment},
				{"## Blocked\n- none\nSTATUS: partial", CauseCapability},
			} {
				rec := &Record{Path: t.TempDir()}
				judge(env, &Task{Project: t.TempDir(), Access: brief.AccessEdit}, rec, brief.ParseReport(tc.report), nil, &brief.Brief{}, attemptBaseline{})
				var gate Gate
				if err := json.Unmarshal(rec.Verification.Gate, &gate); err != nil || gate.Cause != tc.cause || gate.CauseSource != "rule" {
					t.Fatalf("report=%s gate=%+v error=%v", tc.report, gate, err)
				}
			}
		})
	}
}

func TestCauseStateLimitsAndRedaction(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	var request causeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "SECRET") {
			t.Error("unredacted state")
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"answers":{"cause":{"choice":"environment"}}}`)
	}))
	defer server.Close()
	long := "SECRET" + strings.Repeat("x", maxDiffBytes)
	env := &Env{Roster: &roster.Roster{Router: config.Router{BaseURL: server.URL}}, Project: &config.Project{Redact: []string{"SECRET"}}}
	rec := &Record{Outcome: Outcome{Status: brief.StatusPartial, FailureClass: FailBad}, Verification: Verification{Checks: []verify.CheckResult{{Output: long}}}}
	ensureCause(env, &Task{Project: t.TempDir(), Title: long}, rec, &brief.Brief{Goal: long, Done: long}, brief.ParseReport("## Blocked\n"+long), attemptBaseline{})
	for _, field := range []string{request.State.Brief.Title, request.State.Brief.Goal, request.State.Brief.Done, request.State.Blocked, request.State.FailedChecks} {
		if len(field) > maxDiffBytes+len("[redacted]") || len(field) < maxDiffBytes {
			t.Fatalf("state field length: %d", len(field))
		}
	}
}

func TestBadResultWithoutReportGetsCauseBeforeItEnds(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	root, folder := causeRoot(t), t.TempDir()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var request causeRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Questions) != 1 {
			t.Errorf("request=%+v error=%v", request, err)
		}
		if rec, err := LoadRecord(folder); err == nil && rec.State == "ended" {
			t.Error("Dispatch ended before it got its Cause")
		}
		_, _ = io.WriteString(w, `{"answers":{"cause":{"choice":"environment"}}}`)
	}))
	defer server.Close()
	bin := filepath.Join(t.TempDir(), "lane")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"status\":\"SUCCESS\",\"denied_actions\":[\"tool denied\"]}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &Env{Root: root, Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}, Reg: harness.New(map[string]string{"agy": bin})}
	task := &Task{Project: root, Folder: folder, Access: brief.AccessRead, Route: &router.Route{Harness: "agy"}, WallLimit: "1m", IdleLimit: "1m"}
	rec := runOnce(env, task, &brief.Brief{}, 1, nil)
	if rec.Outcome.FailureClass != FailBad || rec.ResolveCause() != CauseEnvironment || requests != 1 || rec.State != "ended" {
		t.Fatalf("record=%+v requests=%d", rec, requests)
	}
}
