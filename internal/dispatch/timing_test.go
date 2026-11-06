package dispatch

import (
	"crypto/rand"
	"reflect"
	"testing"
	"time"
)

func TestTimingDefaults(t *testing.T) {
	if dispatchPollInterval != time.Second || soloWakeDelay != time.Second || relayPollInterval != time.Second || relayDeadline != 10*time.Minute {
		t.Fatalf("timing defaults: %s, %s, %s, %s", dispatchPollInterval, soloWakeDelay, relayPollInterval, relayDeadline)
	}
	for _, tc := range []struct {
		class string
		wait  time.Duration
	}{{FailLane, 5 * time.Second}, {FailTransient, 30 * time.Second}} {
		if wait, again := retryWait(Outcome{FailureClass: tc.class}); !again || wait != tc.wait {
			t.Fatalf("retry %s = %s, %v", tc.class, wait, again)
		}
	}
	if (&Env{}).RetryDelay != 0 {
		t.Fatal("retry default must use the failure wait")
	}
	if (RelayRequest{}).PollInterval != 0 {
		t.Fatal("Relay wait default must use the shared interval")
	}
	if reflect.ValueOf(taskIDBytes).Pointer() != reflect.ValueOf(rand.Read).Pointer() {
		t.Fatal("Task IDs must use crypto/rand.Read")
	}
	if dispatchTicks != nil || reflect.ValueOf(dispatchNow).Pointer() != reflect.ValueOf(time.Now).Pointer() {
		t.Fatal("Dispatch limits must use the system clock and ticker")
	}
	if reflect.ValueOf(soloWakeWait).Pointer() != reflect.ValueOf(time.Sleep).Pointer() {
		t.Fatal("Solo wake-ups must use the system wait")
	}
	// The test setters change only the values, and restore the defaults.
	restoreWake := SetSoloWakeForTest(time.Millisecond, func(time.Duration) {})
	restoreRelay := SetRelayPollForTest(time.Millisecond)
	if soloWakeDelay != time.Millisecond || relayPollInterval != time.Millisecond || reflect.ValueOf(soloWakeWait).Pointer() == reflect.ValueOf(time.Sleep).Pointer() {
		t.Fatal("test setters did not change the values")
	}
	restoreRelay()
	restoreWake()
	if soloWakeDelay != time.Second || relayPollInterval != time.Second || reflect.ValueOf(soloWakeWait).Pointer() != reflect.ValueOf(time.Sleep).Pointer() {
		t.Fatalf("test setters did not restore the defaults: %s, %s", soloWakeDelay, relayPollInterval)
	}
	restoreWake = SetSoloWakeForTest(time.Millisecond, nil)
	if reflect.ValueOf(soloWakeWait).Pointer() != reflect.ValueOf(time.Sleep).Pointer() {
		t.Fatal("a nil wait must keep the current wait")
	}
	restoreWake()
}

func fastDispatchTimers(t *testing.T) {
	t.Helper()
	oldPoll, oldRelay := dispatchPollInterval, relayPollInterval
	dispatchPollInterval, relayPollInterval = 2*time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { dispatchPollInterval, relayPollInterval = oldPoll, oldRelay })
}
