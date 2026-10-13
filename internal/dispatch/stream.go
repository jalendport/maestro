package dispatch

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jalendport/maestro/internal/harness"
)

const streamLineLimit = 64 << 20

func pumpStream(src io.Reader, dst *os.File, parser harness.Parser, activity func()) []Event {
	reader := bufio.NewReaderSize(src, 1<<20)
	var events []Event
	var line []byte
	var size int64
	for {
		part, err := reader.ReadSlice('\n')
		if len(part) > 0 && part[len(part)-1] == '\n' {
			part = part[:len(part)-1]
		}
		size += int64(len(part))
		if size <= streamLineLimit {
			line = append(line, part...)
		} else {
			line = nil
			activity()
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if size > streamLineLimit {
			events = append(events, Event{At: time.Now(), Kind: "stream", Text: fmt.Sprintf("dropped line of %d bytes (limit %d bytes)", size, streamLineLimit)})
		} else if size > 0 || err == nil {
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if dst != nil {
				_, _ = dst.Write(line)
				_, _ = dst.Write([]byte{'\n'})
			}
			if parser.Line(line) {
				activity()
			}
		}
		line = line[:0]
		size = 0
		if err != nil {
			if err != io.EOF {
				events = append(events, Event{At: time.Now(), Kind: "stream", Text: "stream read: " + err.Error()})
			}
			return events
		}
	}
}

func laneFinished(done <-chan error, stop func()) (error, bool) {
	select {
	case err := <-done:
		stop()
		return err, true
	default:
		return nil, false
	}
}
