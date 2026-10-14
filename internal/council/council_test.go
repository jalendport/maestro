package council

import (
	"testing"

	"github.com/jalendport/maestro/internal/adapter"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/discovery"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func TestRequestedAgySeatRequiresHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MAESTRO_HOME", t.TempDir())
	defaults, _, err := config.ParseFile(roster.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	env := &dispatch.Env{Roster: roster.Merge(defaults, &config.File{}), Reg: harness.New(map[string]string{"agy": "/must-not-launch"}),
		Disc: &discovery.Cache{Harness: map[string]discovery.Record{"agy": {Path: "/must-not-launch", Version: "1.3.4", Models: []harness.Model{{ID: "gemini-3.8-flash"}}}}}}
	seats, _, err := pickSeats(env, &Council{Level: roster.LevelSome}, Opts{Lanes: []string{"agy-flash:high"}})
	want := "family agy-flash fails pre-flight: agy needs its Maestro hook: run maestro adapter install agy"
	if len(seats) != 0 || err == nil || err.Error() != want {
		t.Fatalf("seats=%+v err=%v, want %s", seats, err, want)
	}
	if result := adapter.Install("agy", home, "skill"); result.Err != nil {
		t.Fatal(result.Err)
	}
	seats, _, err = pickSeats(env, &Council{Level: roster.LevelSome}, Opts{Lanes: []string{"agy-flash:high"}})
	if err != nil || len(seats) != 1 || seats[0].Family != "agy-flash" || seats[0].Effort != "high" || seats[0].Model != "gemini-3.8-flash" {
		t.Fatalf("seats=%+v err=%v", seats, err)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain question",
			in:   "How Does Routing Work",
			want: "how-does-routing-work",
		},
		{
			name: "punctuation and spaces collapse",
			in:   "hello, world!!  how?",
			want: "hello-world-how",
		},
		{
			name: "at most six words",
			in:   "one two three four five six seven eight",
			want: "one-two-three-four-five-six",
		},
		{
			name: "empty question",
			in:   "",
			want: "council",
		},
		{
			name: "all punctuation",
			in:   "???",
			want: "council",
		},
		{
			name: "leading and trailing hyphens trimmed",
			in:   "  -- Hello, World! --  ",
			want: "hello-world",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Slugify(tc.in); got != tc.want {
				t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
