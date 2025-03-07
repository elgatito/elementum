package bittorrent

import (
	"errors"
	"io"
	"os"
	"time"

	lt "github.com/ElementumOrg/libtorrent-go"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
)

// MemoryFile ...
type MemoryFile struct {
	t    *Torrent
	tf   *TorrentFS
	s    lt.MemoryStorage
	f    *File
	path string

	opMu sync.Mutex
	mu   sync.Mutex

	pos int64
}

// NewMemoryFile ...
func NewMemoryFile(t *Torrent, tf *TorrentFS, storage lt.MemoryStorage, file *File, path string) *MemoryFile {
	// log.Debugf("New memory file: %v", path)
	return &MemoryFile{
		t:    t,
		tf:   tf,
		s:    storage,
		f:    file,
		path: path,
	}
}

// Close ...
func (mf *MemoryFile) Close() (err error) {
	// log.Debugf("Closing memory file: %#v", mf.path)

	return
}

// Read ...
func (mf *MemoryFile) Read(b []byte) (n int, err error) {
	return
}

// ReadPiece ...
func (mf *MemoryFile) ReadPiece(b []byte, piece int, pieceOffset int) (n int, err error) {
	defer perf.ScopeTimer()()

	mf.opMu.Lock()
	defer mf.opMu.Unlock()

	// Check if torrent is closed to avoid panics
	if mf.t.Closer.IsSet() {
		return
	}

	n = mf.s.Read(b, len(b), piece, pieceOffset)

	if n == -1 {
		err = io.ErrShortBuffer
		return
	} else if len(b) != n {
		err = io.ErrUnexpectedEOF
		return
	}

	mf.mu.Lock()
	defer mf.mu.Unlock()

	mf.pos += int64(n)
	if mf.pos > mf.f.Size {
		log.Debugf("EOF POS: piece=%d, pos=%d, size=%d, n=%d", piece, mf.pos, mf.f.Size, n)
		err = io.EOF
	} else if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}

	return
}

// Seek ...
func (mf *MemoryFile) Seek(off int64, whence int) (ret int64, err error) {
	mf.opMu.Lock()
	defer mf.opMu.Unlock()

	mf.mu.Lock()
	defer mf.mu.Unlock()
	switch whence {
	case io.SeekStart:
		mf.pos = off
	case io.SeekCurrent:
		mf.pos += off
	case io.SeekEnd:
		mf.pos = mf.f.Size + off
	default:
		err = errors.New("bad whence")
	}
	ret = mf.pos

	return
}

// Readdir ...
func (mf *MemoryFile) Readdir(count int) (ret []os.FileInfo, err error) {
	// log.Debugf("Memory. Read: %#v", count)
	return
}

// Stat ...
func (mf *MemoryFile) Stat() (ret os.FileInfo, err error) {
	// log.Debugf("Memory. Stat: %#v, F: %#v", mf, mf.f)
	return mf, nil
}

// Name ...
func (mf *MemoryFile) Name() string {
	return mf.f.Name
}

// Size ...
func (mf *MemoryFile) Size() int64 {
	return mf.f.Size
}

// Mode ...
func (mf *MemoryFile) Mode() os.FileMode {
	return 0777
}

// ModTime ...
func (mf *MemoryFile) ModTime() time.Time {
	return time.Now()
}

// IsDir ...
func (mf *MemoryFile) IsDir() bool {
	return false
}

// Sys ...
func (mf *MemoryFile) Sys() interface{} {
	return nil
}
