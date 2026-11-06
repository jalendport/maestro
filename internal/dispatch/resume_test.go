package dispatch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jalendport/maestro/internal/brief"
	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/roster"
)

func TestResumedTaskIsJudgedOnItsOwnFiles(t *testing.T) {
	for _, committed := range []bool{false, true} {
		for _, sharedEarlier := range []bool{false, true} {
			t.Run(fmt.Sprintf("committed=%t/sharedEarlier=%t", committed, sharedEarlier), func(t *testing.T) {
				t.Setenv("MAESTRO_HOME", t.TempDir())
				t.Setenv("TYPESAFE_API_KEY", "test-key")
				root, folder := diffRepo(t), t.TempDir()
				write := func(path, contents string) {
					t.Helper()
					if err := os.WriteFile(filepath.Join(root, path), []byte(contents), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				first, err := takeBaseline(root, folder)
				if err != nil {
					t.Fatal(err)
				}
				write("tracked.txt", "first attempt\n")
				if sharedEarlier {
					write("shared.txt", "first attempt\n")
				}
				rec := &Record{Path: folder, Attempt: 1, Verification: Verification{Changed: first.Changed(root)}}
				if err := rec.Save(); err != nil {
					t.Fatal(err)
				}
				archiveAttempt(folder, 1)
				write("other.txt", "other Task only\n")
				write("shared.txt", "other Task\n")
				if committed {
					for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "commit", "-qm", "Other Task"}} {
						if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
							t.Fatalf("git %v: %v %s", args, err, out)
						}
					}
				}
				second, err := takeBaseline(root, folder)
				if err != nil {
					t.Fatal(err)
				}
				if err := second.inherit(folder, 2); err != nil {
					t.Fatal(err)
				}
				write("current.txt", "current attempt\n")
				if !sharedEarlier {
					write("shared.txt", "current attempt\n")
				}
				requests := make(chan string, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var payload struct{ State struct{ Diff string } }
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Error(err)
					}
					requests <- payload.State.Diff
					fmt.Fprint(w, `{"answers":{"satisfies":{"noul":1},"verdict":{"choice":"pass"},"risk":{"score":1,"legend":{"1":"low"}}}}`)
				}))
				defer server.Close()
				env := &Env{Roster: &roster.Roster{Router: config.Router{Backend: "jev", BaseURL: server.URL}}}
				want := []string{"current.txt", "shared.txt", "tracked.txt"}
				task := &Task{Project: root, Folder: folder, Access: brief.AccessEdit, Touch: want}
				rec = &Record{Path: folder, Attempt: 2}
				judge(env, task, rec, brief.ParseReport("STATUS: done\n"), []string{"true"}, &brief.Brief{Done: "The Task changes its own files."}, second)
				if !reflect.DeepEqual(rec.Verification.Changed, want) || len(rec.Verification.TouchViolations) != 0 || rec.Outcome.Outcome != OutcomeVerified {
					t.Fatalf("resumed Record: %+v", rec)
				}
				var diff string
				select {
				case diff = <-requests:
				default:
					t.Fatal("gate received no request")
				}
				for _, path := range want {
					if !strings.Contains(diff, path) {
						t.Fatalf("gate diff lacks %s: %s", path, diff)
					}
				}
				if strings.Contains(diff, "other.txt") || strings.Contains(diff, "other Task only") {
					t.Fatalf("gate saw another Task's file: %s", diff)
				}
				if got := DiffFor(root, rec); got != diff {
					t.Fatalf("saved diff = %q, gate diff = %q", got, diff)
				}
				archiveAttempt(folder, 2)
				write("later.txt", "another Task\n")
				third, err := takeBaseline(root, folder)
				if err != nil {
					t.Fatal(err)
				}
				if err := third.inherit(folder, 3); err != nil {
					t.Fatal(err)
				}
				if got := third.Changed(root); !reflect.DeepEqual(got, want) {
					t.Fatalf("third attempt Changed = %v, want %v", got, want)
				}
			})
		}
	}
}
