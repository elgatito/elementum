// +build !windows

package ignore lockfile

import (
	"os"
	"syscall"
)

func isRunning(pid int) (bool, error) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false, nil
	}
	return true, nil
}
