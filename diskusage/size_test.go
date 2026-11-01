//go:build !windows

package diskusage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSizeOnDiskSparse(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	info, _ := f.Stat()
	if got := SizeOnDisk(info); got >= info.Size() {
		t.Errorf("SizeOnDisk = %d, want less than %d for sparse file", got, info.Size())
	}
}
