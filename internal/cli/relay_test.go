package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
)

func controlFolder(t *testing.T, folder string) string {
	t.Helper()
	control, err := config.ControlDirForFolder(folder)
	if err != nil {
		t.Fatal(err)
	}
	return control
}

type relayReply struct {
	id  string
	err error
}

func answerOpenRelay(control, answer string) <-chan relayReply {
	done := make(chan relayReply, 1)
	go func() {
		until := time.Now().Add(5 * time.Second)
		for time.Now().Before(until) {
			var request dispatch.RelayRequest
			b, err := os.ReadFile(filepath.Join(control, "relay.json"))
			if err == nil && json.Unmarshal(b, &request) == nil && request.ID != "" {
				err = dispatch.WriteRelayAnswer(control, answer, request.ID)
				done <- relayReply{request.ID, err}
				return
			}
			time.Sleep(time.Millisecond)
		}
		done <- relayReply{err: fmt.Errorf("Relay did not open")}
	}()
	return done
}

func TestRelayCommandUsesEnvironmentAndProjectLookup(t *testing.T) {
	for _, useEnv := range []bool{false, true} {
		t.Setenv("MAESTRO_HOME", t.TempDir())
		root := t.TempDir()
		folder := filepath.Join(root, ".agents", "maestro", "runs", "r-test", "abcd")
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(cwd) })
		t.Setenv("MAESTRO_TASK_FOLDER", "")
		id := "abcd"
		if useEnv {
			t.Setenv("MAESTRO_TASK_FOLDER", folder)
			id = "not-found"
		}
		control := controlFolder(t, folder)
		if err := os.MkdirAll(control, 0o700); err != nil {
			t.Fatal(err)
		}
		reply := answerOpenRelay(control, "allow")
		cmd := (&app{}).relayCmd()
		if !cmd.Hidden {
			t.Fatal("relay is visible")
		}
		cmd.SetIn(strings.NewReader(`{"tool_name":"Bash","tool_input":{}}`))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs([]string{id})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if got := <-reply; got.err != nil {
			t.Fatal(got.err)
		}
		if !strings.Contains(out.String(), `"behavior":"allow"`) {
			t.Fatalf("output: %s", out.String())
		}
	}
}

func TestWaitRelayEventsPrintOnce(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = old })
	rec := &dispatch.Record{Task: "abcd", Events: []dispatch.Event{{Kind: "relay", Text: "0123456789abcdef Bash pwd"}, {Kind: "relay-answer", Text: "deny"}}}
	seen := map[string]bool{}
	printRelayEvents(rec, seen)
	printRelayEvents(rec, seen)
	w.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	r.Close()
	if strings.Count(out.String(), "maestro answer abcd allow|deny --relay 0123456789abcdef") != 1 || strings.Count(out.String(), "relay-answer") != 1 {
		t.Fatalf("output: %s", out.String())
	}
}

func TestCleanRemovesRunAndCouncilControlFolders(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	var controls []string
	for _, tc := range []struct{ parent, run, task string }{
		{config.RunsDir(root), "r-0001", "abcd"},
		{config.CouncilsDir(root), "review", "agy-flash"},
	} {
		folder := filepath.Join(tc.parent, tc.run, tc.task)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		control := controlFolder(t, folder)
		controls = append(controls, filepath.Dir(control))
		if err := os.MkdirAll(control, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"read"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(filepath.Dir(folder), old, old); err != nil {
			t.Fatal(err)
		}
	}
	seat := filepath.Join(config.CouncilsDir(root), "review", "round-2", "agy-flash")
	if err := os.MkdirAll(seat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seat, dispatch.FileTask), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	control := controlFolder(t, seat)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(control, "lane.json"), []byte(`{"access":"read"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(config.CouncilsDir(root), "review"), old, old); err != nil {
		t.Fatal(err)
	}
	command := (&app{}).cleanCmd()
	command.SetArgs([]string{"--older-than", "1d"})
	captureStdout(t, command.Execute)
	controls = append(controls, filepath.Join(config.RunsDir(root), "r-0001"), filepath.Join(config.CouncilsDir(root), "review"), control, filepath.Dir(controls[0]))
	for _, folder := range controls {
		if _, err := os.Stat(folder); !os.IsNotExist(err) {
			t.Fatalf("clean left %s: %v", folder, err)
		}
	}
}

func TestCleanKeepsOtherProjectAndLiveArtifacts(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root, other := t.TempDir(), t.TempDir()
	t.Chdir(root)
	old := time.Now().Add(-48 * time.Hour)
	var removed, kept []string
	for _, project := range []string{root, other} {
		for _, artifact := range []string{"r-0001", "r-0002"} {
			folder := filepath.Join(config.RunsDir(project), artifact, "abcd")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			control := controlFolder(t, folder)
			if err := os.MkdirAll(control, 0o700); err != nil {
				t.Fatal(err)
			}
			policy := filepath.Join(control, "lane.json")
			if err := os.WriteFile(policy, []byte(project), 0o600); err != nil {
				t.Fatal(err)
			}
			if artifact == "r-0001" {
				if err := os.Chtimes(filepath.Dir(folder), old, old); err != nil {
					t.Fatal(err)
				}
			}
			if project == root && artifact == "r-0001" {
				removed = append(removed, policy, folder)
			} else {
				kept = append(kept, policy, folder)
			}
		}
	}
	command := (&app{}).cleanCmd()
	command.SetArgs([]string{"--older-than", "1d"})
	captureStdout(t, command.Execute)
	for _, path := range removed {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("clean kept %s: %v", path, err)
		}
	}
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("clean removed %s: %v", path, err)
		}
	}
}

func TestCleanRemovesOnlyOldUnkeyedControlEntries(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	old := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"r-0001", "old-file", "recent", "0123456789abcdef", "0123456789abcdeg"} {
		path := filepath.Join(config.Home(), "control", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if name == "old-file" {
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if name != "recent" {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	command := (&app{}).cleanCmd()
	command.SetArgs([]string{"--older-than", "1d"})
	captureStdout(t, command.Execute)
	for _, name := range []string{"r-0001", "old-file", "recent", "0123456789abcdef", "0123456789abcdeg"} {
		_, err := os.Stat(filepath.Join(config.Home(), "control", name))
		keep := name == "recent" || name == "0123456789abcdef"
		if keep && err != nil || !keep && !os.IsNotExist(err) {
			t.Fatalf("control entry %s: keep=%v, error=%v", name, keep, err)
		}
	}
}

func TestAnswerRequiresOpenRelayID(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	root := t.TempDir()
	t.Chdir(root)
	folder := filepath.Join(config.RunsDir(root), "r-0001", "abcd")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	control := controlFolder(t, folder)
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, open, id, want string }{
		{"no request", "", "1111111111111111", "no open relay for task abcd"},
		{"late A while B is open", "2222222222222222", "1111111111111111", "relay 1111111111111111 is no longer open; the open relay is 2222222222222222"},
		{"missing flag", "2222222222222222", "", "required flag(s) \"relay\" not set"},
		{"matching B", "2222222222222222", "2222222222222222", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.open != "" {
				b, _ := json.Marshal(dispatch.RelayRequest{ID: tc.open})
				if err := os.WriteFile(filepath.Join(control, "relay.json"), b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := (&app{}).answerCmd()
			args := []string{"abcd", "allow"}
			if tc.id != "" {
				args = append(args, "--relay", tc.id)
			}
			cmd.SetArgs(args)
			var result error
			captureStdout(t, func() error { result = cmd.Execute(); return nil })
			if tc.want != "" {
				if result == nil || result.Error() != tc.want {
					t.Fatalf("error = %v, want %s", result, tc.want)
				}
				if _, err := os.Stat(filepath.Join(control, "answer-allow")); !os.IsNotExist(err) {
					t.Fatalf("refused answer wrote a marker: %v", err)
				}
			} else {
				b, err := os.ReadFile(filepath.Join(control, "answer-allow"))
				if result != nil || err != nil || string(b) != tc.id {
					t.Fatalf("answer = %q, %v, %v", b, err, result)
				}
			}
		})
	}
}
