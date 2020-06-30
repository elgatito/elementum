// +build windows

package diskusage

import (
	"syscall"
	"unsafe"
)

var (
	kernel32, _            = syscall.LoadLibrary("Kernel32.dll")
	pGetDiskFreeSpaceEx, _ = syscall.GetProcAddress(kernel32, "GetDiskFreeSpaceExW")
)

// disk usage of path/disk
func DiskUsage(path string) (*DiskStatus, error) {
	lpDirectoryName, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	lpFreeBytesAvailable := int64(0)
	lpTotalNumberOfBytes := int64(0)
	lpTotalNumberOfFreeBytes := int64(0)
	syscall.Syscall6(uintptr(pGetDiskFreeSpaceEx), 4,
		uintptr(unsafe.Pointer(lpDirectoryName)),
		uintptr(unsafe.Pointer(&lpFreeBytesAvailable)),
		uintptr(unsafe.Pointer(&lpTotalNumberOfBytes)),
		uintptr(unsafe.Pointer(&lpTotalNumberOfFreeBytes)), 0, 0)
	status := &DiskStatus{
		All:  lpTotalNumberOfBytes,
		Free: lpFreeBytesAvailable,
	}
	status.Used = status.All - status.Free
	return status, nil
}
