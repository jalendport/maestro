// Package ledger appends finished Dispatch, Council, and Run rows to the one
// global JSONL file. Rows are written once and never updated.
package ledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"

	"github.com/jalendport/maestro/internal/config"
)

// Append writes one row. The row must carry its own "kind" field.
func Append(row any) error {
	if err := config.EnsureHome(); err != nil {
		return err
	}
	f, err := os.OpenFile(config.LedgerFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(row)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// Each calls fn with every row as raw JSON. A missing ledger is empty.
func Each(fn func(raw json.RawMessage) error) error {
	f, err := os.Open(config.LedgerFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if err := fn(json.RawMessage(append([]byte(nil), line...))); err != nil {
			return err
		}
	}
	return sc.Err()
}
