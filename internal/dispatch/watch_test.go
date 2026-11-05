package dispatch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/router"
)

func TestWatchHarnessStreams(t *testing.T) {
	for _, tc := range []struct {
		name, fixture, tool, want string
	}{
		{"claude", "claude-stream.jsonl", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read"}]}}`, "OK\ntool: Read\n"},
		{"codex", "codex-stream.jsonl", `{"type":"item.started","item":{"type":"mcp_tool_call","tool":"read_file"}}`, "OK\ntool: read_file\n"},
		{"grok", "grok-stream.jsonl", "", "tool: read_file\n2\n"},
		{"agy", "agy-out.json", "", "OK\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			folder := t.TempDir()
			stream, err := os.ReadFile(filepath.Join("..", "harness", "testdata", tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if tc.tool != "" {
				stream = append(stream, []byte(tc.tool+"\n")...)
			}
			if err := os.WriteFile(filepath.Join(folder, FileOutput), stream, 0o644); err != nil {
				t.Fatal(err)
			}
			rec := &Record{Path: folder, State: "ended", Route: &router.Route{Harness: tc.name}}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := Watch(context.Background(), folder, &out); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("View=%q, want %q", out.String(), tc.want)
			}
		})
	}
}

func TestWatchSkipsUnknownStreamLines(t *testing.T) {
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		t.Run(name, func(t *testing.T) {
			folder := t.TempDir()
			if err := os.WriteFile(filepath.Join(folder, FileOutput), []byte("unknown text\n{\"type\":\"future_event\",\"data\":\"hidden\"}\n{broken\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			rec := &Record{Path: folder, State: "ended", Route: &router.Route{Harness: name}}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := Watch(context.Background(), folder, &out); err != nil || out.Len() != 0 {
				t.Fatalf("View=%q, error=%v", out.String(), err)
			}
		})
	}
}

func TestWatchGrokJoinsTextChunks(t *testing.T) {
	for _, boundary := range []string{"", `{"type":"end"}`, `{"type":"usage"}`, `{"type":"tool_call","toolName":"read_file"}`} {
		t.Run(boundary, func(t *testing.T) {
			folder := t.TempDir()
			stream := "{\"type\":\"text\",\"data\":\"## Ans\"}\n{\"type\":\"text\",\"data\":\"wer\\nSTATUS: done\"}\n" + boundary + "\n"
			if err := os.WriteFile(filepath.Join(folder, FileOutput), []byte(stream), 0o600); err != nil {
				t.Fatal(err)
			}
			rec := &Record{Path: folder, State: "ended", Route: &router.Route{Harness: "grok"}, Events: []Event{{Kind: "verdict", Text: "answered"}}}
			if err := rec.Save(); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := Watch(context.Background(), folder, &out); err != nil {
				t.Fatal(err)
			}
			want := "## Answer\nSTATUS: done\n"
			if strings.Contains(boundary, "tool_call") {
				want += "tool: read_file\n"
			}
			want += "verdict answered\n"
			if out.String() != want {
				t.Fatalf("View = %q, want %q", out.String(), want)
			}
		})
	}
}

type watchLines chan string

func (w watchLines) Write(p []byte) (int, error) {
	w <- string(p)
	return len(p), nil
}

func TestWatchFollowsGrowingStreamUntilRecordEnds(t *testing.T) {
	folder := t.TempDir()
	rec := &Record{Path: folder, State: "running", Route: &router.Route{Harness: "codex"}, Events: []Event{{Kind: "start", Text: "codex"}}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	stream, err := os.Create(filepath.Join(folder, FileOutput))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lines := make(watchLines, 20)
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, folder, lines) }()
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("line=%q, want %q", got, want)
			}
		case <-ctx.Done():
			t.Fatal("View did not print " + want)
		}
	}
	expect("start codex\n")
	for _, text := range []string{"first", "second"} {
		if _, err := fmt.Fprintf(stream, "{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":%q}}\n", text); err != nil {
			t.Fatal(err)
		}
		expect(text + "\n")
	}
	for _, event := range []Event{{Kind: "relay", Text: "id Bash"}, {Kind: "relay-answer", Text: "id allow"}, {Kind: "check", Text: "go test: ok"}} {
		rec.Events = append(rec.Events, event)
		if err := rec.Save(); err != nil {
			t.Fatal(err)
		}
		expect(strings.TrimSpace(event.Kind+" "+event.Text) + "\n")
	}
	select {
	case err := <-done:
		t.Fatalf("View ended before Dispatch: %v", err)
	default:
	}
	if _, err := stream.WriteString(`{"type":"item.completed","item":{"type":"agent_message","text":"last"}}`); err != nil {
		t.Fatal(err)
	}
	rec.State = "ended"
	rec.Events = append(rec.Events, Event{Kind: "verdict", Text: "verified"})
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	expect("last\n")
	expect("verdict verified\n")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("View did not exit")
	}
	if len(lines) != 0 {
		t.Fatal("View repeated output")
	}
}
