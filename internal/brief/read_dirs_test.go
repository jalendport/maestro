package brief

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadDirsParseAndRoundTrip(t *testing.T) {
	b, err := ParseText("---\ntitle: Read reference files\nread_dirs: [~/reference, ../boilerplate]\n---\n## Goal\nRead.\n## Done\nRead.\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"~/reference", "../boilerplate"}
	if !reflect.DeepEqual(b.ReadDirs, want) {
		t.Fatal(b.ReadDirs)
	}
	path := filepath.Join(t.TempDir(), "brief.md")
	if err := Write(b, path); err != nil {
		t.Fatal(err)
	}
	again, err := Parse(path)
	if err != nil || !reflect.DeepEqual(again.ReadDirs, want) {
		t.Fatalf("round trip = %+v, %v", again, err)
	}
	p, err := ParsePlanText("---\nread_dirs: [~/reference]\n---\n## Read reference files\n- read_dirs: [../boilerplate]\n### Goal\nRead.\n### Done\nRead.\n")
	if err != nil || !reflect.DeepEqual(p.Tasks[0].ReadDirs, want) {
		t.Fatalf("Plan = %+v, %v", p, err)
	}
}

func TestReadDirsRejectOverlap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, pair := range [][2]string{
		{"../reference", "../reference"},
		{"../reference", "../reference/cache"},
		{"../reference/cache", "../reference"},
		{"../reference/./cache/..", "../reference"},
		{"~/reference", filepath.Join(os.Getenv("HOME"), "reference")},
	} {
		fields := "dirs: [" + pair[0] + "]\nread_dirs: [" + pair[1] + "]\n"
		for _, src := range []string{
			"---\ntitle: Read reference files\n" + fields + "---\n## Goal\nRead.\n## Done\nRead.\n",
			"---\n" + fields + "---\n## Read reference files\n### Goal\nRead.\n### Done\nRead.\n",
			"---\ndirs: [" + pair[0] + "]\n---\n## Read reference files\n- read_dirs: [" + pair[1] + "]\n### Goal\nRead.\n### Done\nRead.\n",
			"---\nread_dirs: [" + pair[1] + "]\n---\n## Read reference files\n- dirs: [" + pair[0] + "]\n### Goal\nRead.\n### Done\nRead.\n",
		} {
			var err error
			if strings.Contains(src, "### Goal") {
				_, err = ParsePlanText(src)
			} else {
				_, err = ParseText(src)
			}
			if err == nil || !strings.Contains(err.Error(), "read_dirs") || !strings.Contains(err.Error(), "dirs entry") || !strings.Contains(err.Error(), pair[0]) || !strings.Contains(err.Error(), pair[1]) {
				t.Fatalf("overlap %v: %v", pair, err)
			}
		}
	}
	if err := ValidateDirs([]string{"../reference-cache"}, []string{"../reference"}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDirs([]string{root}, []string{alias}); err == nil {
		t.Fatal("symlink overlap accepted")
	}
}
