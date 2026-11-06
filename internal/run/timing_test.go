package run

import (
	"testing"
	"time"
)

func TestSupervisorTimingDefaults(t *testing.T) {
	if supervisorPollInterval != 2*time.Second {
		t.Fatalf("supervisor poll interval = %s, want 2s", supervisorPollInterval)
	}
	restore := SetPollIntervalForTest(time.Millisecond)
	if supervisorPollInterval != time.Millisecond {
		t.Fatalf("test poll interval = %s", supervisorPollInterval)
	}
	restore()
	if supervisorPollInterval != 2*time.Second {
		t.Fatalf("restored poll interval = %s, want 2s", supervisorPollInterval)
	}
}
