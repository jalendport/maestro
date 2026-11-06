package worktree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLandedEmptyIdentity(t *testing.T) {
	root := repository(t)
	assertLanded(t, root, "missing", true, "")
	if err := RemoveLanded(root, "missing", "master"); err != nil {
		t.Fatal(err)
	}
}

func assertLanded(t *testing.T, root, id string, want bool, reasonPart string) {
	t.Helper()
	got, reason, err := Landed(root, id, "master")
	if err != nil || got != want || !strings.Contains(reason, reasonPart) || (want && reason != "") || (!want && reason == "") {
		t.Fatalf("Landed = %v, %q, %v; want %v, containing %q", got, reason, err, want, reasonPart)
	}
}

func TestLandedBranchHistory(t *testing.T) {
	root := repository(t)
	r := openRun(t, root)
	w := addTask(t, r, "task")
	assertLanded(t, root, r.ID, true, "")
	commitTask(t, w, "task.txt", "task\n")
	assertLanded(t, root, r.ID, false, "branch "+w.Branch+" has commits that did not land")
	tgit(t, root, "merge", "--ff-only", w.Branch)
	assertLanded(t, root, r.ID, true, "")
}

func TestLandedUncommittedPaths(t *testing.T) {
	for _, kind := range []string{"edit", "staged", "untracked", "delete", "rename", "symlink", "mode", "index differs"} {
		t.Run(kind, func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "task")
			switch kind {
			case "edit", "staged", "index differs":
				write(t, w.Dir, "base.txt", "landed\n")
				if kind != "edit" {
					tgit(t, w.Dir, "add", "base.txt")
				}
			case "untracked":
				write(t, w.Dir, "new file\n[1].txt", "landed\n")
			case "delete":
				if err := os.Remove(filepath.Join(w.Dir, "delete.txt")); err != nil {
					t.Fatal(err)
				}
			case "rename":
				tgit(t, w.Dir, "mv", "delete.txt", "renamed.txt")
			case "symlink":
				if err := os.Symlink("base.txt", filepath.Join(w.Dir, "link")); err != nil {
					t.Fatal(err)
				}
			case "mode":
				tgit(t, root, "config", "core.filemode", "true")
				if err := os.Chmod(filepath.Join(w.Dir, "base.txt"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			assertLanded(t, root, r.ID, false, "uncommitted changes in ")
			switch kind {
			case "edit", "staged", "index differs":
				write(t, root, "base.txt", "landed\n")
			case "untracked":
				write(t, root, "new file\n[1].txt", "landed\n")
			case "delete":
				tgit(t, root, "rm", "delete.txt")
			case "rename":
				tgit(t, root, "mv", "delete.txt", "renamed.txt")
			case "symlink":
				if err := os.Symlink("base.txt", filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(filepath.Join(root, "base.txt"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			tgit(t, root, "add", "-A")
			tgit(t, root, "commit", "-m", "Land files by hand")
			assertLanded(t, root, r.ID, true, "")
			if kind == "index differs" {
				write(t, w.Dir, "base.txt", "unlanded index\n")
				tgit(t, w.Dir, "add", "base.txt")
				write(t, w.Dir, "base.txt", "landed\n")
				assertLanded(t, root, r.ID, false, "uncommitted changes in ")
			}
		})
	}
}

func TestRemoveLandedAllWorktrees(t *testing.T) {
	for _, id := range []string{"r-0010", "d-r-0010-abcd"} {
		t.Run(id, func(t *testing.T) {
			root := repository(t)
			write(t, root, ".gitignore", "vendor/\n")
			tgit(t, root, "add", ".gitignore")
			tgit(t, root, "commit", "-m", "Ignore dependency folder")
			write(t, root, "vendor/dependency", "shared\n")
			r, err := Open(root, id)
			if err != nil {
				t.Fatal(err)
			}
			w := addTask(t, r, "task")
			read, err := r.Read("read")
			if err != nil {
				t.Fatal(err)
			}
			landing, _, err := r.LandingWorktree()
			if err != nil {
				t.Fatal(err)
			}
			// A sibling identity must survive the removal.
			other, err := Open(root, id+"0")
			if err != nil {
				t.Fatal(err)
			}
			otherTree := addTask(t, other, "task")
			write(t, w.Dir, "base.txt", "landed\n")
			write(t, root, "base.txt", "landed\n")
			tgit(t, root, "add", "base.txt")
			tgit(t, root, "commit", "-m", "Land edit by hand")
			write(t, root, "user.txt", "keep user work\n")
			assertLanded(t, root, id, true, "")
			if err := RemoveLanded(root, id, "refs/heads/master"); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{r.dir, w.Dir, read.Dir, landing} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("path remains: %s: %v", path, err)
				}
			}
			if branches := tgit(t, root, "for-each-ref", "--format=%(refname)", "refs/heads/maestro/"+id+"/"); branches != "" {
				t.Fatalf("branches remain: %s", branches)
			}
			for _, path := range []string{otherTree.Dir, filepath.Join(root, "user.txt"), filepath.Join(root, "vendor/dependency")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestLandedPathsCommittedAfterBase(t *testing.T) {
	for _, kind := range []string{"edit", "staged", "untracked", "delete", "staged delete", "symlink", "executable"} {
		t.Run(kind, func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "task")
			path := "base.txt"
			if kind == "untracked" || kind == "symlink" {
				path = "new file\n[1].txt"
			}
			for _, dir := range []string{w.Dir, root} {
				switch kind {
				case "delete", "staged delete":
					if err := os.Remove(filepath.Join(dir, path)); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink("base.txt", filepath.Join(dir, path)); err != nil {
						t.Fatal(err)
					}
				default:
					write(t, dir, path, "landed\n")
					if kind == "executable" {
						tgit(t, root, "config", "core.filemode", "true")
						if err := os.Chmod(filepath.Join(dir, path), 0o755); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if strings.HasPrefix(kind, "staged") {
				tgit(t, w.Dir, "add", "-A")
			}
			assertLanded(t, root, r.ID, false, "uncommitted changes in ")
			tgit(t, root, "add", "-A")
			tgit(t, root, "commit", "-m", "Land files by hand")
			if kind == "symlink" {
				if err := os.Remove(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
			}
			write(t, root, path, "later edit\n")
			tgit(t, root, "add", "-A")
			tgit(t, root, "commit", "-m", "Change landed files again")
			assertLanded(t, root, r.ID, true, "")
			if kind == "staged" {
				write(t, w.Dir, path, "never landed\n")
				tgit(t, w.Dir, "add", path)
				write(t, w.Dir, path, "later edit\n")
				assertLanded(t, root, r.ID, false, "uncommitted changes in ")
				if err := RemoveLanded(root, r.ID, "master"); err == nil {
					t.Fatal("removed an identity with unlanded staged content")
				}
			} else if err := RemoveLanded(root, r.ID, "master"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLandedRejectsContentBeforeBase(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "disk", true: "index"}[staged], func(t *testing.T) {
			root := repository(t)
			write(t, root, "base.txt", "at base\n")
			tgit(t, root, "add", "base.txt")
			tgit(t, root, "commit", "-m", "Change original content")
			r := openRun(t, root)
			w := addTask(t, r, "task")
			write(t, w.Dir, "base.txt", "base\n")
			if staged {
				tgit(t, w.Dir, "add", "base.txt")
				write(t, w.Dir, "base.txt", "at base\n")
			}
			assertLanded(t, root, r.ID, false, "uncommitted changes in ")
		})
	}
}

func TestLandedHistoryRequiresPathAndMode(t *testing.T) {
	for _, kind := range []string{"path", "mode", "deletion before base"} {
		t.Run(kind, func(t *testing.T) {
			root := repository(t)
			if kind == "deletion before base" {
				tgit(t, root, "rm", "base.txt")
				tgit(t, root, "commit", "-m", "Delete original path")
				write(t, root, "base.txt", "restored\n")
				tgit(t, root, "add", "base.txt")
				tgit(t, root, "commit", "-m", "Restore original path")
			}
			r := openRun(t, root)
			w := addTask(t, r, "task")
			if kind == "deletion before base" {
				if err := os.Remove(filepath.Join(w.Dir, "base.txt")); err != nil {
					t.Fatal(err)
				}
			} else {
				path := "base.txt"
				if kind == "path" {
					path = "other.txt"
				}
				write(t, root, path, "landed\n")
				tgit(t, root, "add", path)
				tgit(t, root, "commit", "-m", "Commit content elsewhere")
				write(t, w.Dir, "base.txt", "landed\n")
				if kind == "mode" {
					tgit(t, root, "config", "core.filemode", "true")
					if err := os.Chmod(filepath.Join(w.Dir, "base.txt"), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				write(t, root, path, "later edit\n")
				tgit(t, root, "add", path)
				tgit(t, root, "commit", "-m", "Change content again")
			}
			assertLanded(t, root, r.ID, false, "uncommitted changes in ")
		})
	}
}

func TestLandedBaseFallback(t *testing.T) {
	for _, kind := range []string{"saved base", "missing saved run", "empty saved base", "no run branch"} {
		t.Run(kind, func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "task")
			write(t, w.Dir, "base.txt", "landed\n")
			write(t, root, "base.txt", "landed\n")
			tgit(t, root, "add", "base.txt")
			tgit(t, root, "commit", "-m", "Land edit by hand")
			if kind == "saved base" {
				tgit(t, root, "branch", "-f", r.Branch, "master")
			}
			write(t, root, "base.txt", "later edit\n")
			tgit(t, root, "add", "base.txt")
			tgit(t, root, "commit", "-m", "Change landed file again")
			switch kind {
			case "missing saved run", "no run branch":
				if err := os.Remove(filepath.Join(r.dir, "run.json")); err != nil {
					t.Fatal(err)
				}
			case "empty saved base":
				write(t, r.dir, "run.json", "{}")
			}
			if kind == "no run branch" {
				tgit(t, root, "branch", "-D", r.Branch)
				assertLanded(t, root, r.ID, false, "uncommitted changes in ")
				write(t, w.Dir, "base.txt", "later edit\n")
			}
			assertLanded(t, root, r.ID, true, "")
		})
	}
}

func TestRemoveLandedRefusesWithoutChanges(t *testing.T) {
	for _, blocker := range []string{"branch", "task", "read", "landing"} {
		t.Run(blocker, func(t *testing.T) {
			root := repository(t)
			r := openRun(t, root)
			w := addTask(t, r, "task")
			read, err := r.Read("read")
			if err != nil {
				t.Fatal(err)
			}
			landing, _, err := r.LandingWorktree()
			if err != nil {
				t.Fatal(err)
			}
			switch blocker {
			case "branch":
				commitTask(t, w, "new.txt", "unlanded\n")
			case "task":
				write(t, w.Dir, "base.txt", "unlanded\n")
			case "read":
				write(t, read.Dir, "new.txt", "unlanded\n")
			case "landing":
				write(t, landing, "base.txt", "unlanded\n")
			}
			beforeTrees := tgit(t, root, "worktree", "list", "--porcelain")
			beforeRefs := tgit(t, root, "show-ref")
			beforeDirty := make(map[string]string)
			for _, dir := range []string{root, w.Dir, read.Dir, landing} {
				beforeDirty[dir] = tgit(t, dir, "status", "--porcelain")
			}
			assertLanded(t, root, r.ID, false, "")
			if err := RemoveLanded(root, r.ID, "master"); err == nil {
				t.Fatal("removed an unlanded identity")
			}
			if beforeTrees != tgit(t, root, "worktree", "list", "--porcelain") || beforeRefs != tgit(t, root, "show-ref") {
				t.Fatal("refusal changed Worktrees or branches")
			}
			for dir, before := range beforeDirty {
				if got := tgit(t, dir, "status", "--porcelain"); got != before {
					t.Fatalf("refusal changed %s", dir)
				}
			}
		})
	}
}

func TestLandedInvalidInput(t *testing.T) {
	root := repository(t)
	for _, input := range []struct{ id, branch string }{{"../bad", "master"}, {"safe", "missing"}} {
		if _, _, err := Landed(root, input.id, input.branch); err == nil {
			t.Fatalf("accepted invalid input: %+v", input)
		}
		if err := RemoveLanded(root, input.id, input.branch); err == nil {
			t.Fatalf("removed invalid input: %+v", input)
		}
	}
}
