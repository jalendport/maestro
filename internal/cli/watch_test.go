package cli

import (
	"strings"
	"testing"
)

func TestWatchCommandRemoved(t *testing.T) {
	root := (&app{}).build()
	root.SetArgs([]string{"watch", "abcd"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("watch error = %v", err)
	}
	root = (&app{}).build()
	root.SetArgs([]string{"help"})
	help := captureStdout(t, root.Execute)
	if strings.Contains(help, "watch <task|folder>") {
		t.Fatalf("help has watch: %s", help)
	}
}
