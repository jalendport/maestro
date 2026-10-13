package council

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/roster"
)

func TestMergeRedactsQuestionAndFindings(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- body
		_, _ = io.WriteString(w, `{"answers":{"f0":{"noul":1}}}`)
	}))
	defer server.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "seat.md"), []byte("## Answer\nanswer\n## Findings\n- MARKED\n## Concerns\nnone\nPOSITION: confirmed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Council{Project: root, Question: "MARKED", Seats: []*Seat{{Family: "test", Rounds: []SeatRound{{Round: 1, Status: "ok", Report: "seat.md"}}}}}
	env := &dispatch.Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}, Project: &config.Project{Redact: []string{"MARKED"}}}
	merge(env, c)
	if c.FilterSkipped != "" {
		t.Fatal(c.FilterSkipped)
	}
	body := <-requests
	if strings.Contains(string(body), "MARKED") {
		t.Fatalf("unredacted request: %s", body)
	}
	var payload struct {
		State struct {
			Question string
			Findings map[string]string
		}
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.State.Question != "[redacted]" || payload.State.Findings["f0"] != "[redacted]" {
		t.Fatalf("request: %s", body)
	}
	if len(c.Findings) != 1 || c.Findings[0].Text != "MARKED" {
		t.Fatal("local finding changed")
	}
}
