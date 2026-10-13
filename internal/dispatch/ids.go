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
// It reserves the folder before returning. Since the signature has no error
// return, an allocation error panics rather than returning an unreserved id.
func NextRunID(root string) string {
	runsDir := config.RunsDir(root)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		panic(fmt.Errorf("reserve Run id: %w", err))
	}
	max := 0
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		panic(fmt.Errorf("reserve Run id: %w", err))
	}
	for _, e := range entries {
		if m := runIDRe.FindStringSubmatch(e.Name()); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > max {
				max = n
			}
		}
	}
	for {
		max++
		id := fmt.Sprintf("r-%04d", max)
		if err := os.Mkdir(filepath.Join(runsDir, id), 0o755); err != nil {
			if os.IsExist(err) {
				continue
			}
			panic(fmt.Errorf("reserve Run id: %w", err))
		}
		return id
	}
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
