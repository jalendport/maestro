package jev

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestKeyMissing(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "")

	if _, err := Key(); !errors.Is(err, ErrNoKey) {
		t.Fatalf("Key() error = %v, want ErrNoKey", err)
	}
}

func TestSaveKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MAESTRO_HOME", home)
	t.Setenv("TYPESAFE_API_KEY", "")

	if err := SaveKey("abc"); err != nil {
		t.Fatal(err)
	}
	key, err := Key()
	if err != nil {
		t.Fatal(err)
	}
	if key != "abc" {
		t.Fatalf("Key() = %q, want %q", key, "abc")
	}
	info, err := os.Stat(filepath.Join(home, "env"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("env file mode = %04o, want 0600", mode)
	}
}

func TestKeyEnvironmentWins(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "env-wins")

	if err := SaveKey("file-value"); err != nil {
		t.Fatal(err)
	}
	key, err := Key()
	if err != nil {
		t.Fatal(err)
	}
	if key != "env-wins" {
		t.Fatalf("Key() = %q, want %q", key, "env-wins")
	}
}
