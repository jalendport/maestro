package dispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
)

func TestPendingRelayIgnoresResolvedRequests(t *testing.T) {
	for _, state := range []string{"open", "marker", "answer file", "record", "expired", "no owner", "new Relay"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			folder := filepath.Join(t.TempDir(), ".agents", "maestro", "runs", "r-0001", "abcd")
			control, err := config.ControlDirForFolder(folder)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{folder, control} {
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			request := RelayRequest{ID: "current", AskedAt: time.Now(), ToolName: "Bash"}
			if state == "expired" {
				request.AskedAt = time.Now().Add(-relayDeadline)
			}
			if err := writeRelayJSON(control, "relay.json", request); err != nil {
				t.Fatal(err)
			}
			if state != "no owner" {
				if err := writeRelayJSON(control, "relay.lock", relayLock{PID: os.Getpid(), At: time.Now(), ID: request.ID}); err != nil {
					t.Fatal(err)
				}
			}
			switch state {
			case "marker", "new Relay":
				id := request.ID
				if state == "new Relay" {
					id = "previous"
				}
				if err := WriteRelayAnswer(control, "allow", id); err != nil {
					t.Fatal(err)
				}
			case "answer file":
				if err := writeRelayJSON(control, "relay-answer-"+request.ID+".json", relayAnswer{Request: request, Answer: "allow"}); err != nil {
					t.Fatal(err)
				}
			case "record":
				rec := &Record{Path: folder, Permission: Permission{Relays: []json.RawMessage{json.RawMessage(`{"id":"current","answer":"allow"}`)}}}
				if err := rec.Save(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := PendingRelay(folder)
			want := state == "open" || state == "new Relay"
			if err != nil || (got != nil) != want {
				t.Fatalf("PendingRelay = %+v, %v; want open=%v", got, err, want)
			}
			if state == "marker" {
				b, err := os.ReadFile(filepath.Join(control, "answer-allow"))
				if err != nil || string(b) != request.ID {
					t.Fatalf("query consumed the answer: %q, %v", b, err)
				}
			}
		})
	}
}
