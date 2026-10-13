// Package ledger appends finished Dispatch, Council, and Run rows to the one
// global JSONL file. Rows are written once and never updated.
package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/ui"
)

// Append writes one row. The row must carry its own "kind" field.
func Append(row any) error {
	if err := config.EnsureHome(); err != nil {
		return err
	}
	lock, err := os.OpenFile(config.LedgerFile()+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
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
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) > 0 {
			if !json.Valid(line) {
				if errors.Is(err, io.EOF) {
					ui.Warnf("ledger: skipped an incomplete last line")
					return nil
				}
				return fmt.Errorf("ledger: invalid JSON row")
			}
			if err := fn(json.RawMessage(line)); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}
