package dispatch

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jalendport/maestro/internal/config"
)

var runIDRe = regexp.MustCompile(`^r-(\d{4,})$`)

// NextRunID allocates the next r-NNNN in the project's runs folder.
// It reserves the folder before returning.
func NextRunID(root string) (string, error) {
	runsDir := config.RunsDir(root)
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return "", fmt.Errorf("reserve Run id: %w", err)
	}
	max := 0
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return "", fmt.Errorf("reserve Run id: %w", err)
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
			return "", fmt.Errorf("reserve Run id: %w", err)
		}
		return id, nil
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
	runs, _ := RunFolders(root)
	for _, r := range runs {
		if !r.IsDir() {
			continue
		}
		if r.Name() == id {
			folder := filepath.Join(config.RunsDir(root), r.Name())
			if _, err := os.Stat(filepath.Join(folder, "run.json")); !os.IsNotExist(err) {
				return "", false
			}
			tasks, _ := os.ReadDir(folder)
			found := ""
			for _, task := range tasks {
				path := filepath.Join(folder, task.Name())
				if _, err := LoadTask(path); task.IsDir() && err == nil {
					if found != "" {
						return "", false
					}
					found = path
				}
			}
			return found, found != ""
		}
		p := filepath.Join(config.RunsDir(root), r.Name(), id)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// RunFolders returns Run folders in descending numeric id order.
func RunFolders(root string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(config.RunsDir(root))
	if err != nil {
		return nil, err
	}
	runs := entries[:0]
	for _, entry := range entries {
		if entry.IsDir() {
			runs = append(runs, entry)
		}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(runs[i].Name(), "r-"))
		b, _ := strconv.Atoi(strings.TrimPrefix(runs[j].Name(), "r-"))
		return a > b
	})
	return runs, nil
}
