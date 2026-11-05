package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/router"
)

func TestWatchCommand(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	folder := t.TempDir()
	rec := &dispatch.Record{Path: folder, State: "ended", Route: &router.Route{Harness: "grok"}}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, dispatch.FileOutput), []byte("{\"type\":\"text\",\"data\":\"visible\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := (&app{}).build()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"watch", folder})
	if err := root.Execute(); err != nil || out.String() != "visible\n" {
		t.Fatalf("output=%q error=%v", out.String(), err)
	}
	root = (&app{}).build()
	root.SetArgs([]string{"help"})
	help := captureStdout(t, root.Execute)
	if !strings.Contains(help, "watch <task|folder>") {
		t.Fatalf("help lacks watch: %s", help)
	}
}
