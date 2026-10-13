package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLoadUnknownKeys(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	if err := os.WriteFile(UserFile(), []byte("[router]\nbacknd = 'rules'\n[accounts.test]\nharness = 'codex'\nbilling = 'subscription'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, unknown, err := LoadUser()
	if err != nil || !slices.Equal(unknown, []string{"router.backnd"}) {
		t.Fatalf("%v %v", unknown, err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(ProjectFile(root)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ProjectFile(root), []byte("check = ['go test ./...']\nchecks = ['go vet ./...']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(project.Unknown, []string{"check"}) || !slices.Equal(project.Checks, []string{"go vet ./..."}) {
		t.Fatalf("%+v", project)
	}
}
