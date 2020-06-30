// +build !windows

package diskusage

import (
	"syscall"
)

// DiskUsage shows disk usage of path/disk
func DiskUsage(path string) (*DiskStatus, error) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return nil, err
	}
	status := &DiskStatus{
		All:  int64(fs.Blocks) * int64(fs.Bsize),
		Free: int64(fs.Bfree) * int64(fs.Bsize),
	}
	status.Used = status.All - status.Free
	return status, nil
}
