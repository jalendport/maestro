package proc

import (
	"errors"
	"testing"
)

func TestQuote(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"plain word", []string{"maestro"}, "maestro"},
		{"space", []string{"hello world"}, "'hello world'"},
		{"single quote", []string{"it's"}, `'it'\''s'`},
		{"empty argument", []string{""}, "''"},
		{"several arguments", []string{"maestro", "hello world", "it's", ""}, `maestro 'hello world' 'it'\''s' ''`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Quote(c.args); got != c.want {
				t.Errorf("Quote(%q) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func TestCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		ok   bool
	}{
		{"exit error", &ExitError{Code: 2}, 2, true},
		{"plain error", errors.New("failed"), 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, ok := Code(c.err); got != c.want || ok != c.ok {
				t.Errorf("Code(%v) = (%d, %t), want (%d, %t)", c.err, got, ok, c.want, c.ok)
			}
		})
	}
}
