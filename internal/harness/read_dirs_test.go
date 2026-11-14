package harness

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestClaudeReferenceRules(t *testing.T) {
	for _, access := range []string{"read", "edit"} {
		l := Launch{Access: access, Root: "/project", RunFolder: "/run", Dirs: []string{"/cache"}, ReadDirs: []string{"/reference"}, ReadFiles: []string{"/sibling/one.go", "/sibling/[two].go"}}
		command, err := (&Claude{bin: "/bin/claude"}).Build(l)
		if err != nil {
			t.Fatal(err)
		}
		args := command.Cmd.Args
		for _, rule := range []string{"Read(//reference)", "Read(//reference/**)", "Read(//sibling/one.go)", `Read(//sibling/\[two\].go)`, "Edit(//reference/**)", "Edit(//sibling/one.go)"} {
			if !slices.Contains(args, rule) {
				t.Fatalf("%s missing %s: %q", access, rule, args)
			}
		}
		for i, arg := range args {
			if arg == "--add-dir" && args[i+1] != "/cache" {
				t.Fatalf("reference became writable: %q", args)
			}
			if strings.Contains(arg, "Read(//sibling/**)") || strings.Contains(arg, "Grep(//") || strings.Contains(arg, "Glob(//") {
				t.Fatalf("broad or unsupported read rule: %s", arg)
			}
		}
		var settings struct {
			Sandbox struct {
				Filesystem struct {
					AllowWrite []string `json:"allowWrite"`
					DenyWrite  []string `json:"denyWrite"`
				} `json:"filesystem"`
			} `json:"sandbox"`
			Hooks map[string][]struct{ Matcher string } `json:"hooks"`
		}
		if err := json.Unmarshal([]byte(args[slices.Index(args, "--settings")+1]), &settings); err != nil {
			t.Fatal(err)
		}
		for _, path := range append(l.ReadDirs, l.ReadFiles...) {
			if !slices.Contains(settings.Sandbox.Filesystem.DenyWrite, path) || slices.Contains(settings.Sandbox.Filesystem.AllowWrite, path) {
				t.Fatalf("reference write boundary: %+v", settings.Sandbox.Filesystem)
			}
		}
		matcher := regexp.MustCompile("^(?:" + settings.Hooks["PreToolUse"][0].Matcher + ")$")
		for _, tool := range []string{"Read", "Glob", "Grep", "Write", "Edit", "NotebookEdit", "Bash"} {
			if !matcher.MatchString(tool) {
				t.Fatal("hook does not check " + tool)
			}
		}
	}
}

func TestOtherHarnessReferenceFlagsStayUnchanged(t *testing.T) {
	root := t.TempDir()
	reg := New(map[string]string{"codex": "/bin/codex", "grok": "/bin/grok", "agy": "/bin/agy"})
	for _, name := range []string{"codex", "grok", "agy"} {
		for _, access := range []string{"read", "edit"} {
			h, _ := reg.Get(name)
			l := Launch{Access: access, Root: root, RunFolder: filepath.Join(root, "run")}
			before, err := h.Build(l)
			if err != nil {
				t.Fatal(err)
			}
			l.ReadDirs, l.ReadFiles = []string{"/reference"}, []string{"/sibling/one.go"}
			after, err := h.Build(l)
			if err != nil || !reflect.DeepEqual(before.Flags, after.Flags) || before.Enforcement != after.Enforcement {
				t.Fatalf("%s %s: reference changed launch: %+v, %v", name, access, after, err)
			}
		}
	}
}
