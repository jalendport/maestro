package proc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SnapshotProcess reads identity without starting a command on the hook path.
func SnapshotProcess(pid int) (Process, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return Process{}, err
	}
	line := string(data)
	start, end := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
	if start < 0 || end <= start {
		return Process{}, fmt.Errorf("no process record for %d", pid)
	}
	fields := strings.Fields(line[end+1:])
	if len(fields) < 20 {
		return Process{}, fmt.Errorf("no process start time for %d", pid)
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return Process{}, err
	}
	return Process{Parent: parent, Executable: line[start+1 : end], Started: fields[19]}, nil
}
