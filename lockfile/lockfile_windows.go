// +build windows

package lockfile

import "syscall"

// For some reason these consts don't exist in syscall.
const (
	InvalidParameter = 87
	CodeStillActive  = 259
)

func isRunning(pid int) (bool, error) {
	procHnd, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, true, uint32(pid))
	if err != nil {
		if scerr, ok := err.(syscall.Errno); ok {
			if uintptr(scerr) == InvalidParameter {
				return false, nil
			}
		}
	}

	var code uint32
	err = syscall.GetExitCodeProcess(procHnd, &code)
	if err != nil {
		return false, err
	}

	return code == CodeStillActive, nil
}
