package worktree

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEligibleChecks(t *testing.T) {
	root := repository(t)
	for _, tc := range []struct{ check, word string }{
		{"ddev exec vendor/bin/phpunit", "ddev"},
		{"docker compose run --rm app test", "docker"},
		{"./vendor/bin/sail test", "./vendor/bin/sail"},
		{"env -i -u HOME CI='some value' docker-compose run app test", "docker-compose"},
		{"CI=1 env NO_COLOR=1 lando test", "lando"},
		{"APP_ENV=test sail test", "sail"},
		{"go test ./...", ""},
		{"env -u MAESTRO_HOME CGO_ENABLED=0 go test ./...", ""},
		{"printf 'docker compose'", ""},
	} {
		t.Run(tc.check, func(t *testing.T) {
			ok, reason := Eligible(root, nil, []string{tc.check}, nil)
			want := ""
			if tc.word != "" {
				want = "checks run in a container bound to the project folder (" + tc.word + ")"
			}
			if ok != (want == "") || reason != want {
				t.Fatalf("Eligible = %v, %q; want %q", ok, reason, want)
			}
		})
	}
	tgit(t, root, "checkout", "--detach")
	if ok, reason := Eligible(root, nil, nil, nil); ok || reason != "HEAD is not on a branch" {
		t.Fatalf("detached: %v, %s", ok, reason)
	}
	tgit(t, root, "checkout", "--orphan", "empty")
	if ok, reason := Eligible(root, nil, nil, nil); ok || reason != "tree has no git history" {
		t.Fatalf("empty: %v, %s", ok, reason)
	}
}

func TestEnvironmentAndProjectLinks(t *testing.T) {
	root := repository(t)
	write(t, root, ".gitignore", ".agents/\n.env*\nstorage/\nconfig/license.key\n")
	write(t, root, ".agents/maestro.toml", "worktree_links = ['storage', 'config/license.key']\n")
	for _, name := range []string{".env", ".env.local", "storage/framework/cache/file", "config/license.key", "not-ignored"} {
		write(t, root, name, "local data\n")
	}
	tgit(t, root, "add", ".gitignore", "not-ignored")
	tgit(t, root, "commit", "-m", "Add link rules")
	r := openRun(t, root)
	w := addTask(t, r, "links")
	for _, name := range []string{".env", ".env.local", "storage", "config/license.key"} {
		got, err := os.Readlink(filepath.Join(w.Dir, name))
		if err != nil || got != filepath.Join(root, name) {
			t.Fatalf("link %s = %q, %v", name, got, err)
		}
	}
	if dirty, err := Dirty(w.Dir); err != nil || len(dirty) != 0 {
		t.Fatalf("linked Worktree status: %v, %v", dirty, err)
	}
	for _, name := range []string{".env", "storage", "config/license.key"} {
		patterns := []string{name + "/**"}
		if name == ".env" {
			patterns = []string{name}
		}
		links, err := Link(root, t.TempDir(), patterns)
		if err != nil || !reflect.DeepEqual(links.Refused, []string{name}) {
			t.Fatalf("refused %s: %+v, %v", name, links, err)
		}
		ok, reason := Eligible(root, [][]string{patterns}, nil, []string{"storage", "config/license.key"})
		if ok || !strings.Contains(reason, "touches "+name) {
			t.Fatalf("writable link: %v, %q", ok, reason)
		}
	}
	preview := t.TempDir()
	if _, err := link(root, preview, []string{"not-ignored", "missing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(preview, "not-ignored")); !os.IsNotExist(err) {
		t.Fatalf("linked a tracked file: %v", err)
	}
}
