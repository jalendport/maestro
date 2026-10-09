package ui

import (
	"io"
	"os"
	"testing"
)

func TestOutfQuiet(t *testing.T) {
	for _, tt := range []struct {
		name  string
		quiet bool
		want  string
	}{
		{name: "quiet", quiet: true, want: ""},
		{name: "output", quiet: false, want: "hello world 42\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, quiet := os.Stdout, Quiet
			t.Cleanup(func() {
				os.Stdout, Quiet = stdout, quiet
				reader.Close()
				writer.Close()
			})
			os.Stdout, Quiet = writer, tt.quiet

			Outf("hello %s %d", "world", 42)
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			out, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tt.want {
				t.Fatalf("output = %q, want %q", out, tt.want)
			}
		})
	}
}
