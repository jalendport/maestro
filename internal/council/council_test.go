package council

import (
	"testing"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/dispatch"
	"github.com/jalendport/maestro/internal/harness"
	"github.com/jalendport/maestro/internal/roster"
)

func TestRequestedSeatRefusesBestEffortRead(t *testing.T) {
	defaults, _, err := config.ParseFile(roster.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	env := &dispatch.Env{Roster: roster.Merge(defaults, &config.File{}), Reg: harness.New(map[string]string{"agy": "/must-not-launch"})}
	seats, _, err := pickSeats(env, &Council{Level: roster.LevelSome}, Opts{Lanes: []string{"agy-flash:high"}})
	want := "lane agy-flash cannot take read work: its read shape is best-effort (see maestro roster)"
	if len(seats) != 0 || err == nil || err.Error() != want {
		t.Fatalf("seats=%+v err=%v, want %s", seats, err, want)
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
