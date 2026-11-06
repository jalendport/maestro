package dispatch

import (
	"os"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/router"
	"github.com/jalendport/maestro/internal/verify"
)

func TestPrintFlakyCheck(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "print")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rec := &Record{
		Path: t.TempDir(), Route: &router.Route{},
		Outcome: Outcome{Outcome: OutcomeVerified},
		Verification: Verification{Checks: []verify.CheckResult{
			{Command: "flaky check", OK: true, Flaky: true},
			{Command: "passing check", OK: true},
			{Command: "failed check"},
		}},
	}
	old := os.Stdout
	func() { os.Stdout = file; defer func() { os.Stdout = old }(); Print(rec, false) }()
	printed, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	text := string(printed)
	if strings.Count(text, "ok (flaky: passed on rerun)") != 1 || !strings.Contains(text, "passing check") || !strings.Contains(text, "failed") {
		t.Fatalf("print: %s", text)
	}
}
