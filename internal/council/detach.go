package council

import (
	"fmt"

	"github.com/jalendport/maestro/internal/proc"
)

// Detach starts a Council supervisor that survives the caller's exit.
func Detach(folder string) error {
	err := proc.Detach(folder, "_council", false)
	if failure, ok := err.(*proc.DetachError); ok {
		switch failure.Operation {
		case "find":
			return fmt.Errorf("find Council supervisor executable: %w", failure.Err)
		case "log":
			return fmt.Errorf("create Council supervisor log: %w", failure.Err)
		case "start":
			return fmt.Errorf("start Council supervisor: %w", failure.Err)
		case "pid":
			return fmt.Errorf("write Council supervisor pid: %w", failure.Err)
		}
	}
	return err
}
