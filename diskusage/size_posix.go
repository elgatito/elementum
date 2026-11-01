//go:build !windows

package diskusage

import (
	"io/fs"
	"syscall"
)

// SizeOnDisk returns bytes allocated for a file, less than its size for sparse, partially downloaded torrent files
func SizeOnDisk(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}
