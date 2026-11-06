package cli

import (
	"reflect"
	"testing"
	"time"
)

func TestWaitTimingDefaults(t *testing.T) {
	if waitPollInterval != 2*time.Second || waitProcessGrace != 5*time.Second {
		t.Fatalf("wait defaults: poll %s, grace %s", waitPollInterval, waitProcessGrace)
	}
	if reflect.ValueOf(waitSleep).Pointer() != reflect.ValueOf(time.Sleep).Pointer() {
		t.Fatal("wait must use the system sleep")
	}
}
