package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
)

func TestAppendProcess(t *testing.T) {
	if id := os.Getenv("MAESTRO_LEDGER_TEST_WRITER"); id != "" {
		if _, err := os.Stdout.WriteString("ready\n"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 8; i++ {
			if err := Append(map[string]any{"kind": "test", "writer": id, "row": i, "text": strings.Repeat("x", 1<<16)}); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	t.Setenv("MAESTRO_HOME", t.TempDir())
	lock, err := os.OpenFile(config.LedgerFile()+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var done []chan error
	for _, id := range []string{"1", "2", "3", "4"} {
		cmd := exec.Command(self, "-test.run=^TestAppendProcess$")
		cmd.Env = append(os.Environ(), "MAESTRO_LEDGER_TEST_WRITER="+id)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
			t.Fatalf("child readiness: %q, %v", line, err)
		}
		ch := make(chan error, 1)
		go func() { ch <- cmd.Wait() }()
		done = append(done, ch)
	}
	for i, ch := range done {
		select {
		case err := <-ch:
			t.Fatalf("child %d appended without lock: %v", i, err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if _, err := os.Stat(config.LedgerFile()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ledger written while locked: %v", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	for i, ch := range done {
		select {
		case err := <-ch:
			if err != nil {
				t.Fatalf("child %d: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("child %d did not append", i)
		}
	}
	seen := map[string]map[int]bool{}
	if err := Each(func(raw json.RawMessage) error {
		var row struct {
			Writer string `json:"writer"`
			Row    int    `json:"row"`
			Text   string `json:"text"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if len(row.Text) != 1<<16 {
			t.Fatalf("torn row: %d bytes", len(row.Text))
		}
		if seen[row.Writer] == nil {
			seen[row.Writer] = map[int]bool{}
		}
		if seen[row.Writer][row.Row] {
			t.Fatalf("duplicate row: %+v", row)
		}
		seen[row.Writer][row.Row] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 {
		t.Fatalf("writers: %v", seen)
	}
	for id, rows := range seen {
		if len(rows) != 8 {
			t.Fatalf("writer %s: %d rows", id, len(rows))
		}
	}
}

func TestEachSkipsTornLastLineWithOneWarning(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	if err := os.WriteFile(config.LedgerFile(), []byte("{\"kind\":\"first\"}\n{\"kind\":\"second\"}\n{\"kind\":"), 0o644); err != nil {
		t.Fatal(err)
	}
	warnings, err := os.CreateTemp(t.TempDir(), "warnings")
	if err != nil {
		t.Fatal(err)
	}
	defer warnings.Close()
	original := os.Stderr
	os.Stderr = warnings
	t.Cleanup(func() { os.Stderr = original })
	count := 0
	if err := Each(func(raw json.RawMessage) error {
		if !json.Valid(raw) {
			t.Fatalf("invalid row reached callback: %q", raw)
		}
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	os.Stderr = original
	if _, err := warnings.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(warnings)
	if err != nil || count != 2 || strings.Count(string(output), "warning:") != 1 {
		t.Fatalf("rows: %d, warnings: %q, error: %v", count, output, err)
	}
}

func TestEachKeepsReadErrors(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	if err := Each(func(json.RawMessage) error { t.Fatal("missing ledger produced a row"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LedgerFile(), []byte("broken\n{\"kind\":\"valid\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Each(func(json.RawMessage) error { return nil }); err == nil {
		t.Fatal("invalid interior row was skipped")
	}
	if err := os.WriteFile(config.LedgerFile(), []byte("{\"kind\":\"valid\"}"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := errors.New("callback failed")
	if err := Each(func(json.RawMessage) error { return want }); !errors.Is(err, want) {
		t.Fatalf("callback error lost: %v", err)
	}
}
