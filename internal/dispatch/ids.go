package dispatch

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jalendport/maestro/internal/config"
)

var runIDRe = regexp.MustCompile(`^r-(\d{4,})$`)

// NextRunID allocates the next r-NNNN in the project's runs folder.
func NextRunID(root string) string {
	max := 0
	entries, _ := os.ReadDir(config.RunsDir(root))
	for _, e := range entries {
		if m := runIDRe.FindStringSubmatch(e.Name()); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > max {
				max = n
			}
		}
	}
	return fmt.Sprintf("r-%04d", max+1)
}

// NewTaskID returns a 4-hex id that is free in the Run folder.
func NewTaskID(runDir string) string {
	for {
		b := make([]byte, 2)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		if _, err := os.Stat(filepath.Join(runDir, id)); os.IsNotExist(err) {
			return id
		}
	}
}

// FindTask locates a Task folder by id (or run/task) in the project.
func FindTask(root, id string) (string, bool) {
	if run, task, ok := strings.Cut(id, "/"); ok {
		if !runIDRe.MatchString(run) || task == "" || task == "." || task == ".." || strings.Contains(task, "/") {
			return "", false
		}
		p := filepath.Join(config.RunsDir(root), run, task)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, true
		}
		return "", false
	}
	runs, _ := os.ReadDir(config.RunsDir(root))
	// Newest Run first so a reused id resolves to the latest.
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if !r.IsDir() {
			continue
		}
		if r.Name() == id {
			return filepath.Join(config.RunsDir(root), r.Name()), true
		}
		p := filepath.Join(config.RunsDir(root), r.Name(), id)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, true
		}
	}
	return "", false
}
