package harness

import (
	"reflect"
	"testing"
	"time"
)

func TestAgyEnforcement(t *testing.T) {
	a := &Agy{}
	for _, access := range []string{"read", "edit"} {
		if got := a.Enforcement(access); got != Enforced {
			t.Fatalf("%s: %s, want %s", access, got, Enforced)
		}
	}
	if got := a.Enforcement("other"); got != NoShape {
		t.Fatalf("unknown Access: %s", got)
	}
}

func TestAgyLaunchShapes(t *testing.T) {
	a := &Agy{bin: "/bin/agy"}
	for _, tc := range []struct {
		name, access string
		tab          bool
		args, flags  []string
	}{
		{name: "read one-shot", access: "read",
			args:  []string{"--sandbox", "--output-format", "json", "--model", "gemini-3.8-flash-high", "--print-timeout", "10m0s", "--disable-slash-commands", "-p", "read the brief"},
			flags: []string{"--sandbox"}},
		{name: "edit one-shot", access: "edit",
			args:  []string{"--sandbox", "--output-format", "json", "--model", "gemini-3.8-flash-high", "--print-timeout", "10m0s", "--disable-slash-commands", "--mode", "accept-edits", "--add-dir", "/extra", "--add-dir", "/other dir", "-p", "read the brief"},
			flags: []string{"--sandbox", "--mode", "accept-edits", "--add-dir", "/extra", "--add-dir", "/other dir"}},
		{name: "read tab", access: "read", tab: true,
			args:  []string{"--sandbox", "--model", "gemini-3.8-flash", "--effort", "high", "--prompt-interactive", "read the brief"},
			flags: []string{"--sandbox"}},
		{name: "edit tab", access: "edit", tab: true,
			args:  []string{"--sandbox", "--model", "gemini-3.8-flash", "--effort", "high", "--mode", "accept-edits", "--add-dir", "/extra", "--add-dir", "/other dir", "--prompt-interactive", "read the brief"},
			flags: []string{"--sandbox", "--mode", "accept-edits", "--add-dir", "/extra", "--add-dir", "/other dir"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launch := Launch{Model: "gemini-3.8-flash", Effort: "high", Access: tc.access, Root: "/project",
				Dirs: []string{"/extra", "/other dir"}, Pointer: "read the brief", WallLimit: 10 * time.Minute, Env: []string{"MAESTRO_LANE=1"}}
			build := a.Build
			if tc.tab {
				build = a.BuildTab
			}
			command, err := build(launch)
			if err != nil {
				t.Fatal(err)
			}
			if got := command.Cmd.Args; !reflect.DeepEqual(got, append([]string{a.bin}, tc.args...)) {
				t.Fatalf("arguments: %q, want %q", got, tc.args)
			}
			if !reflect.DeepEqual(command.Flags, tc.flags) {
				t.Fatalf("permission flags: %q, want %q", command.Flags, tc.flags)
			}
			if command.Cmd.Dir != launch.Root || !reflect.DeepEqual(command.Cmd.Env, launch.Env) || command.PromptStdin || command.ReportByText {
				t.Fatalf("launch: %+v", command)
			}
		})
	}
}

func TestAgyLaunchDefaults(t *testing.T) {
	a := &Agy{bin: "/bin/agy"}
	for _, tc := range []struct {
		model, effort string
	}{
		{"gemini-3.8-flash", ""},
		{"gemini-3.8-flash-high", "high"},
	} {
		command, err := a.Build(Launch{Access: "read", Model: tc.model, Effort: tc.effort, Pointer: "brief"})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{a.bin, "--sandbox", "--output-format", "json", "--model", tc.model, "--print-timeout", "0s", "--disable-slash-commands", "-p", "brief"}
		if !reflect.DeepEqual(command.Cmd.Args, want) {
			t.Fatalf("arguments: %q, want %q", command.Cmd.Args, want)
		}
	}
	command, err := a.BuildTab(Launch{Access: "read", Model: "gemini-3.8-flash", Pointer: "brief"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{a.bin, "--sandbox", "--model", "gemini-3.8-flash", "--prompt-interactive", "brief"}
	if !reflect.DeepEqual(command.Cmd.Args, want) {
		t.Fatalf("arguments: %q, want %q", command.Cmd.Args, want)
	}
	for _, build := range []func(Launch) (*Command, error){a.Build, a.BuildTab} {
		if _, err := build(Launch{Access: "other"}); err == nil {
			t.Fatal("unknown Access must fail")
		}
	}
}
