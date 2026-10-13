package proc

import (
	"errors"
	"testing"
)

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
