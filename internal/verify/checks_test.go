package verify

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestChecksStopOnSilenceAndSkipRemaining(t *testing.T) {
	root := t.TempDir()
	start := time.Now()
	results := RunChecks(root, []string{"printf 'started'; sleep 30", "touch must-not-run"}, 100*time.Millisecond, time.Minute)
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond || elapsed > 10*time.Second {
		t.Fatalf("Idle limit took %s", elapsed)
	}
	if len(results) != 2 || results[0].OK || results[0].Skipped || results[0].Reason != "no output for 100ms" || results[0].FailureClass != "stall" || results[0].Output != "started" {
		t.Fatalf("checks: %+v", results)
	}
	if !results[1].Skipped || results[1].Reason != results[0].Reason || results[1].FailureClass != "stall" {
		t.Fatalf("remaining check: %+v", results[1])
	}
	if _, err := os.Stat(filepath.Join(root, "must-not-run")); !os.IsNotExist(err) {
		t.Fatalf("skipped check ran: %v", err)
	}
}

func TestCheckOutputResetsIdleLimit(t *testing.T) {
	for _, redirect := range []string{"", " >&2"} {
		t.Run("redirect="+redirect, func(t *testing.T) {
			command := "for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do printf x" + redirect + "; sleep 0.03; done"
			results := RunChecks(t.TempDir(), []string{command}, 300*time.Millisecond, 10*time.Second)
			if len(results) != 1 || !results[0].OK || results[0].Reason != "" || results[0].Output != strings.Repeat("x", 20) {
				t.Fatalf("active check: %+v", results)
			}
		})
	}
}

func TestCheckExitFailureDoesNotStopNext(t *testing.T) {
	results := RunChecks(t.TempDir(), []string{"printf failure; exit 3", "printf next"}, time.Second, time.Minute)
	if results[0].OK || results[0].Reason != "" || results[0].Skipped || !results[1].OK || results[1].Output != "next" {
		t.Fatalf("checks: %+v", results)
	}
}

func TestCheckRerunsNormalExitOnce(t *testing.T) {
	for _, passes := range []bool{true, false} {
		t.Run(fmt.Sprint(passes), func(t *testing.T) {
			root := t.TempDir()
			command := "printf 'call\\n' >> calls; if [ ! -f first ]; then touch first; printf '%s\\n' '--- FAIL: first attempt'; exit 1; fi; "
			if passes {
				command += "printf passed"
			} else {
				command += "printf 'second failure'; exit 2"
			}
			results := RunChecks(root, []string{command, "printf next"}, time.Minute, time.Minute)
			if len(results) != 2 || results[0].OK != passes || results[0].Flaky != passes || results[0].Reason != "" || !results[1].OK || results[1].Flaky {
				t.Fatalf("checks: %+v", results)
			}
			want := "second failure"
			if passes {
				want = "--- FAIL: first attempt"
			}
			if results[0].Output != want {
				t.Fatalf("output: %q, want %q", results[0].Output, want)
			}
			raw, err := json.Marshal(results[0])
			if err != nil || strings.Contains(string(raw), `"flaky":true`) != passes {
				t.Fatalf("record: %s, %v", raw, err)
			}
			calls, err := os.ReadFile(filepath.Join(root, "calls"))
			if err != nil || string(calls) != "call\ncall\n" {
				t.Fatalf("calls: %q, %v", calls, err)
			}
		})
	}
}

func TestCheckDoesNotRerunStallOrSignal(t *testing.T) {
	for _, command := range []string{"sleep 30", "kill -KILL $$"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			results := RunChecks(root, []string{"printf 'call\\n' >> calls; " + command}, time.Second, time.Minute)
			if len(results) != 1 || results[0].OK || results[0].Flaky {
				t.Fatalf("checks: %+v", results)
			}
			calls, err := os.ReadFile(filepath.Join(root, "calls"))
			if err != nil || string(calls) != "call\n" {
				t.Fatalf("calls: %q, %v", calls, err)
			}
		})
	}
}

func TestCheckDoesNotRerunWithoutEnoughWallBudget(t *testing.T) {
	root := t.TempDir()
	command := "printf 'call\\n' >> calls; sleep 2; printf 'first failure'; exit 1"
	results := RunChecks(root, []string{command, "printf next"}, time.Minute, 3*time.Second)
	if len(results) != 2 || results[0].OK || results[0].Flaky || results[0].Skipped || results[0].Reason != "" || results[0].FailureClass != "bad result" || results[0].Output != "first failure" || !results[1].OK || results[1].Output != "next" {
		t.Fatalf("checks: %+v", results)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil || string(calls) != "call\n" {
		t.Fatalf("calls: %q, %v", calls, err)
	}
}

func TestCheckKeepsFirstFailureWhenRerunStalls(t *testing.T) {
	for _, tt := range []struct {
		name         string
		idle, budget time.Duration
		nextReason   string
	}{
		{"idle", 100 * time.Millisecond, 10 * time.Second, ""},
		{"idle without Wall limit", 100 * time.Millisecond, 0, ""},
		{"wall", time.Minute, 400 * time.Millisecond, "wall limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			command := "printf 'call\\n' >> calls; if [ ! -f first ]; then touch first; printf 'first failure'; exit 3; fi; printf 'rerun output'; sleep 30"
			results := RunChecks(root, []string{command, "printf next"}, tt.idle, tt.budget)
			if len(results) != 2 || results[0].OK || results[0].Flaky || results[0].Skipped || results[0].Reason != "" || results[0].FailureClass != "bad result" || results[0].Output != "first failure" {
				t.Fatalf("first failure: %+v", results)
			}
			if results[1].Skipped || results[1].Reason != tt.nextReason {
				t.Fatalf("next check: %+v", results[1])
			}
			if tt.nextReason == "" {
				if !results[1].OK || results[1].Output != "next" {
					t.Fatalf("next check did not run: %+v", results[1])
				}
			} else if results[1].OK || results[1].FailureClass != "stall" {
				t.Fatalf("next check did not use the shared Wall limit: %+v", results[1])
			}
			calls, err := os.ReadFile(filepath.Join(root, "calls"))
			if err != nil || string(calls) != "call\ncall\n" {
				t.Fatalf("calls: %q, %v", calls, err)
			}
			duration, err := time.ParseDuration(results[0].Duration)
			limit := tt.idle
			if tt.nextReason != "" {
				limit = tt.budget
			}
			if err != nil || duration < limit {
				t.Fatalf("duration does not include the rerun: %s, %v", results[0].Duration, err)
			}
		})
	}
}

func TestFailedCheckKeepsFailureLinesBeforeLongTail(t *testing.T) {
	root := t.TempDir()
	markers := "--- FAIL: TestExample\nFAIL\texample/package\npanic: example\nError: example\nerror: example\n✗ example\nnot ok 1 example\n"
	text := markers + strings.Repeat("detail\n", 15000) + "last line\n"
	if err := os.WriteFile(filepath.Join(root, "output"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	results := RunChecks(root, []string{"cat output; exit 1"}, time.Minute, time.Minute)
	if len(results) != 1 || results[0].OK || !strings.HasPrefix(results[0].Output, markers) || !strings.HasSuffix(results[0].Output, "last line") || len(results[0].Output) > 6001 {
		t.Fatalf("failure output: %+v", results)
	}
	text = strings.Repeat("--- FAIL: TestExample\n", 1000) + strings.Repeat("tail\n", 1000)
	output := recordedOutput(text, true)
	if !strings.HasPrefix(output, "--- FAIL: TestExample\n") || !strings.HasSuffix(output, strings.TrimSpace(text[len(text)-4000:])) || len(output) > 6001 {
		t.Fatalf("failure output length: %d", len(output))
	}
	if output := recordedOutput(text, false); strings.Contains(output, "--- FAIL") || len(output) > 4000 {
		t.Fatalf("passing output length: %d", len(output))
	}
}

func TestChecksShareWallBudget(t *testing.T) {
	start := time.Now()
	results := RunChecks(t.TempDir(), []string{"sleep 0.2", "while true; do printf x; sleep 0.05; done", "printf skipped"}, time.Second, 400*time.Millisecond)
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("Wall limit took %s", elapsed)
	}
	if !results[0].OK || results[1].OK || results[1].Skipped || results[1].Reason != "wall limit" || results[1].FailureClass != "stall" || results[1].Output == "" || !results[2].Skipped || results[2].Reason != "wall limit" {
		t.Fatalf("checks: %+v", results)
	}
}

func stubDocker(t *testing.T, script string) string {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func TestDockerPreflightStopsAfterTenSeconds(t *testing.T) {
	oldLimit := dockerPreflightLimit
	dockerPreflightLimit = 30 * time.Millisecond
	t.Cleanup(func() { dockerPreflightLimit = oldLimit })
	stubDocker(t, "exec sleep 30\n")
	start := time.Now()
	results := RunChecks(t.TempDir(), []string{"docker compose test", "printf skipped"}, time.Minute, time.Minute)
	if elapsed := time.Since(start); elapsed < dockerPreflightLimit || elapsed > time.Second {
		t.Fatalf("Docker pre-flight took %s", elapsed)
	}
	if results[0].OK || results[0].Skipped || results[0].Reason != "Docker does not respond" || results[0].FailureClass != "bad result" || !results[1].Skipped || results[1].Reason != results[0].Reason {
		t.Fatalf("checks: %+v", results)
	}
}

func TestDockerPreflightLimitDefault(t *testing.T) {
	if dockerPreflightLimit != 10*time.Second {
		t.Fatalf("Docker pre-flight limit = %s", dockerPreflightLimit)
	}
}

func TestDockerPreflightRunsOncePerTask(t *testing.T) {
	log := filepath.Join(t.TempDir(), "docker.log")
	t.Setenv("MAESTRO_TEST_DOCKER_LOG", log)
	stubDocker(t, "printf '%s\\n' \"$*\" >>\"$MAESTRO_TEST_DOCKER_LOG\"\n")
	for task := 0; task < 2; task++ {
		results := RunChecks(t.TempDir(), []string{"printf before", "  docker compose test", "docker ps"}, time.Second, time.Minute)
		for _, result := range results {
			if !result.OK {
				t.Fatalf("check: %+v", result)
			}
		}
	}
	raw, err := os.ReadFile(log)
	if err != nil || string(raw) != "info\ncompose test\nps\ninfo\ncompose test\nps\n" {
		t.Fatalf("Docker calls: %q, %v", raw, err)
	}
}

func TestDockerPreflightUsesRemainingBudget(t *testing.T) {
	stubDocker(t, "exec sleep 30\n")
	results := RunChecks(t.TempDir(), []string{"docker ps", "printf skipped"}, time.Second, 100*time.Millisecond)
	if results[0].Reason != "wall limit" || results[0].FailureClass != "stall" || !results[1].Skipped || results[1].Reason != "wall limit" {
		t.Fatalf("checks: %+v", results)
	}
}

func TestDockerPreflightExitFailureSkipsChecks(t *testing.T) {
	log := filepath.Join(t.TempDir(), "docker.log")
	t.Setenv("MAESTRO_TEST_DOCKER_LOG", log)
	stubDocker(t, "printf '%s\\n' \"$*\" >>\"$MAESTRO_TEST_DOCKER_LOG\"\nprintf 'daemon unavailable' >&2\nexit 1\n")
	results := RunChecks(t.TempDir(), []string{"docker ps", "printf skipped"}, time.Second, time.Minute)
	if results[0].OK || results[0].Reason != "Docker does not respond" || results[0].FailureClass != "bad result" || !strings.Contains(results[0].Output, "daemon unavailable") || !results[1].Skipped {
		t.Fatalf("checks: %+v", results)
	}
	calls, err := os.ReadFile(log)
	if err != nil || string(calls) != "info\n" {
		t.Fatalf("Docker calls: %q, %v", calls, err)
	}
}
