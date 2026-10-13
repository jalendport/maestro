package verify

import (
	"testing"
	"time"
)

func TestCheckDeadlineStopsChildren(t *testing.T) {
	start := time.Now()
	results := RunChecks(t.TempDir(), []string{"sleep 2; true"}, 100*time.Millisecond)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline took %s", elapsed)
	}
	if len(results) != 1 || results[0].OK {
		t.Fatalf("results: %+v", results)
	}
}
