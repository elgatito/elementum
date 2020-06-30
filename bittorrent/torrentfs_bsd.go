// +build darwin freebsd dragonfly

package bittorrent

import (
	"os"
	"syscall"
)

// from <sys/stat.h>
const (
	_UF_NODUMP     = 1 << iota
	_UF_IMMUTABLE  = 1 << iota
	_UF_APPEND     = 1 << iota
	_UF_OPAQUE     = 1 << iota
	_UF_COMPRESSED = 1 << iota
	_UF_TRACKED    = 1 << iota
	_UF_HIDDEN     = 1 << iota
)

// Ideally we should read them first, but we control the file
// so it's not that bad anyway.
func unlockFile(file *os.File) error {
	return syscall.Fchflags(int(file.Fd()), 0)
}
