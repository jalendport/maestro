package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/budget"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func captureStdout(t *testing.T, run func() error) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = previous }()
	defer read.Close()
	defer write.Close()
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}

func TestHookCommandScope(t *testing.T) {
	project, outside := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ProjectFile(project), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	for _, tc := range []struct {
		name, harness, cwd, scope string
		deny                      bool
	}{
		{"outside", "claude", outside, "", false},
		{"inside", "claude", project, "", true},
		{"codex outside", "codex", outside, "", false},
		{"codex inside", "codex", project, "", true},
		{"all", "claude", outside, "all", true},
		{"process directory", "claude", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("MAESTRO_TASK_FOLDER", "")
			if tc.scope != "" {
				if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \""+tc.scope+"\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			payload, _ := json.Marshal(map[string]any{"cwd": tc.cwd, "tool_name": "Agent", "tool_input": map[string]any{"subagent_type": "writer"}})
			input, err := os.CreateTemp(t.TempDir(), "payload")
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if _, err := input.Write(payload); err != nil {
				t.Fatal(err)
			}
			if _, err := input.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			previous := os.Stdin
			os.Stdin = input
			defer func() { os.Stdin = previous }()
			command := (&app{}).hookCmd()
			command.SetArgs([]string{tc.harness})
			output := captureStdout(t, command.Execute)
			if strings.Contains(output, `"permissionDecision":"deny"`) != tc.deny {
				t.Fatalf("output = %q, want deny = %v", output, tc.deny)
			}
		})
	}
}

func TestAdapterPrintsScope(t *testing.T) {
	for _, scope := range []string{"projects", "all"} {
		t.Run(scope, func(t *testing.T) {
			t.Setenv("MAESTRO_HOME", t.TempDir())
			t.Setenv("MAESTRO_TASK_FOLDER", "")
			if err := os.WriteFile(config.UserFile(), []byte("[hook]\nscope = \""+scope+"\"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			command := (&app{}).adapterCmd()
			command.SetArgs([]string{"claude"})
			output := captureStdout(t, command.Execute)
			if !strings.Contains(output, "Hook scope: "+scope) || strings.Count(output, "Hook scope:") != 1 {
				t.Fatal(output)
			}
		})
	}
}

func TestDoctorSettingsRows(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	t.Setenv("TYPESAFE_API_KEY", "")
	row := jevRow()
	if row.Name != "jev" || row.Detail != "no key" || !strings.Contains(row.State, "edits will end unverified") {
		t.Fatal(row)
	}
	if err := os.WriteFile(config.EnvFile(), []byte("TYPESAFE_API_KEY=\"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if row = jevRow(); row.Detail != "no key" {
		t.Fatal(row)
	}
	for _, family := range []string{"", "claude-opus"} {
		row = orchestratorRow(family)
		want := family
		if want == "" {
			want = "not set"
		}
		if row.Name != "orchestrator" || row.Detail != want || row.State != "info" {
			t.Fatal(row)
		}
	}
}

func TestBudgetPrintsCodexReserve(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("MAESTRO_TASK_FOLDER", "")
	d, _, err := config.ParseFile(roster.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	r := roster.Merge(d, &config.File{})
	account := r.Accounts["codex"]
	account.Reserve = .35
	r.Accounts["codex"] = account
	b := &budget.File{Accounts: map[string]*budget.Account{}}
	b.Record("codex", []harness.Window{{Kind: "weekly", Used: .4, ResetsAt: time.Now().Add(time.Hour)}}, budget.SourceHarvest)
	env := &dispatch.Env{Roster: r, Budget: b, Disc: &discovery.Cache{}}
	output := captureStdout(t, func() error { return (&app{json: true}).printBudget(env) })
	var data struct {
		Rows []budgetRow `json:"rows"`
	}
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		t.Fatal(err)
	}
	for _, row := range data.Rows {
		if row.Account == "codex" {
			if row.Reserve != "0.35" || row.Headroom != "0.25" {
				t.Fatal(row)
			}
			return
		}
	}
	t.Fatal("Codex budget row missing")
}
