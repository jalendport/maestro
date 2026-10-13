package roster_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/cli"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
)

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	oldArgs, oldOut, oldErr := os.Args, os.Stdout, os.Stderr
	os.Args = append([]string{"maestro"}, args...)
	os.Stdout, os.Stderr = out, out
	defer func() { os.Args, os.Stdout, os.Stderr = oldArgs, oldOut, oldErr }()
	code := cli.Execute("test")
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(data)
}

func setupCLI(t *testing.T, user, project string) {
	t.Helper()
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("SOLO_PROCESS_ID", "")
	root := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.MkdirAll(filepath.Dir(config.ProjectFile(root)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(root), []byte(project), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex", "grok", "agy"} {
		user += "\n[harness." + name + "]\nbin = '/usr/bin/true'\n"
	}
	if err := os.WriteFile(config.UserFile(), []byte(user), 0600); err != nil {
		t.Fatal(err)
	}
	cache := &discovery.Cache{RefreshedAt: time.Now(), Harness: map[string]discovery.Record{}}
	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestRosterDisplaysFamilyWithoutModels(t *testing.T) {
	setupCLI(t, "[families.empty]\nharness = 'codex'\n", "")
	code, out := runCLI(t, "roster", "--json")
	if code != 0 {
		t.Fatalf("%d: %s", code, out)
	}
	var result struct {
		Families []struct {
			Family, State string
			Resolved      string `json:"resolved_model"`
		}
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, f := range result.Families {
		if f.Family == "empty" {
			if f.Resolved != "no model" || !strings.Contains(f.State, "has no model") {
				t.Fatalf("%+v", f)
			}
			return
		}
	}
	t.Fatal("missing Family")
}

func TestDoctorListsUnknownKeysOnce(t *testing.T) {
	setupCLI(t, "[router]\nbacknd = 'rules'\n", "check = ['go test ./...']\n")
	_, out := runCLI(t, "doctor")
	for _, key := range []string{"router.backnd", "check"} {
		if n := strings.Count(out, "unknown key \""+key+"\""); n != 1 {
			t.Fatalf("%s listed %d times: %s", key, n, out)
		}
	}
}
