package verify

import (
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
	stubDocker(t, "printf 'daemon unavailable' >&2\nexit 1\n")
	results := RunChecks(t.TempDir(), []string{"docker ps", "printf skipped"}, time.Second, time.Minute)
	if results[0].OK || results[0].Reason != "Docker does not respond" || results[0].FailureClass != "bad result" || !strings.Contains(results[0].Output, "daemon unavailable") || !results[1].Skipped {
		t.Fatalf("checks: %+v", results)
	}
}
