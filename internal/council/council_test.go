package council

import "testing"

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
