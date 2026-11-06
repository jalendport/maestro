package proc

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// SnapshotProcess reads identity without starting a command on the hook path.
func SnapshotProcess(pid int) (Process, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return Process{}, err
	}
	if int(p.Proc.P_pid) != pid {
		return Process{}, fmt.Errorf("no process record for %d", pid)
	}
	return Process{
		Parent:     int(p.Eproc.Ppid),
		Executable: unix.ByteSliceToString(p.Proc.P_comm[:]),
		Started:    fmt.Sprintf("%d.%06d", p.Proc.P_starttime.Sec, p.Proc.P_starttime.Usec),
	}, nil
}
