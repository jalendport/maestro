package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteJSONAtomic writes indented JSON through a .tmp file with mode 0644.
func WriteJSONAtomic(dir, name string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, name))
}
