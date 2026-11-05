package dispatch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jalendport/maestro/internal/harness"
)

// Watch prints a Dispatch's events and readable stream until its record ends.
func Watch(ctx context.Context, folder string, out io.Writer) error {
	var stream *os.File
	defer func() {
		if stream != nil {
			_ = stream.Close()
		}
	}()
	var reader *bufio.Reader
	var parser harness.Parser
	var pending []byte
	dropped := false
	seen := 0
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		rec, err := LoadRecord(folder)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if rec != nil {
			if parser == nil {
				if rec.Route == nil {
					return fmt.Errorf("Dispatch has no Route: %s", folder)
				}
				h, ok := harness.New(nil).Get(rec.Route.Harness)
				if !ok {
					return fmt.Errorf("unknown Harness %q", rec.Route.Harness)
				}
				parser = h.NewParser()
			}
			if stream == nil {
				stream, err = os.Open(filepath.Join(folder, FileOutput))
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				if stream != nil {
					reader = bufio.NewReaderSize(stream, 64<<10)
				}
			}
			// Start and Relay events precede new stream output. Final checks and
			// the verdict follow the last output, including a line with no newline.
			var after []Event
			for _, event := range rec.Events[seen:] {
				if event.Kind == "check" || event.Kind == "verdict" {
					after = append(after, event)
					continue
				}
				if _, err := fmt.Fprintln(out, strings.TrimSpace(event.Kind+" "+event.Text)); err != nil {
					return err
				}
			}
			seen = len(rec.Events)
			if reader != nil {
				for {
					part, readErr := reader.ReadSlice('\n')
					if len(pending)+len(part) > streamLineLimit {
						pending, dropped = nil, true
					}
					if !dropped {
						pending = append(pending, part...)
					}
					complete := len(part) > 0 && part[len(part)-1] == '\n'
					if complete || (readErr == io.EOF && rec.State == "ended") {
						if !dropped {
							for _, line := range parser.Readable(pending) {
								if _, err := fmt.Fprintln(out, strings.TrimRight(line, "\r\n")); err != nil {
									return err
								}
							}
						}
						pending, dropped = pending[:0], false
					}
					if readErr == io.EOF {
						break
					}
					if readErr != nil && readErr != bufio.ErrBufferFull {
						return readErr
					}
				}
			}
			if rec.State == "ended" {
				for _, line := range parser.Readable(nil) {
					if _, err := fmt.Fprintln(out, strings.TrimRight(line, "\r\n")); err != nil {
						return err
					}
				}
			}
			for _, event := range after {
				if _, err := fmt.Fprintln(out, strings.TrimSpace(event.Kind+" "+event.Text)); err != nil {
					return err
				}
			}
			if rec.State == "ended" {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
