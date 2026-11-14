package dispatch

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/roster"
	"github.com/jalendport/maestro/internal/router"
)

func TestPrepareReadDirs(t *testing.T) {
	t.Setenv("MAESTRO_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	env := &Env{Root: root, Project: &config.Project{}, Roster: &roster.Roster{}}
	b := &brief.Brief{Fields: brief.Fields{Title: "Read reference files", Access: "read", ReadDirs: []string{"~/reference", "../boilerplate"}}}
	task, err := prepareRouted(env, b, &router.Route{}, "r-test", "abcd", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{config.ExpandHome("~/reference"), filepath.Join(root, "../boilerplate")}
	if !reflect.DeepEqual(task.ReadDirs, want) || len(task.Dirs) != 0 {
		t.Fatalf("prepared scope = %+v", task)
	}
	again, err := LoadTask(task.Folder)
	if err != nil || !reflect.DeepEqual(again.ReadDirs, want) {
		t.Fatalf("saved scope = %+v, %v", again, err)
	}
	env.Project.Dirs = []string{want[0]}
	_, err = prepareRouted(env, b, &router.Route{}, "r-test", "efgh", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "read_dirs") || !strings.Contains(err.Error(), "dirs entry") {
		t.Fatalf("project overlap = %v", err)
	}
}
