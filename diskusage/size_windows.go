//go:build windows

package diskusage

import "io/fs"

// SizeOnDisk returns file size.
// shortcut: sparse files count at full size, use GetCompressedFileSizeW if Windows users report inflated numbers
func SizeOnDisk(info fs.FileInfo) int64 {
	return info.Size()
}
