package reader

import (
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("reader")

// PositionReader ...
type PositionReader struct {
	Pos         int64
	Readahead   int64
	Offset      int64
	FileLength  int64
	PieceLength int64
	Pieces      int
}

// PieceRange ...
type PieceRange struct {
	Begin, End int
}

// PiecesRange Calculates the pieces this reader wants downloaded, ignoring the cached
// value at r.pieces.
func (p *PositionReader) PiecesRange() (ret PieceRange) {
	ra := p.Readahead
	if ra < 1 {
		// Needs to be at least 1, because [x, x) means we don't want
		// anything.
		ra = 1
	}
	if ra > p.FileLength-p.Pos {
		ra = p.FileLength - p.Pos
	}
	ret.Begin, ret.End = p.byteRegionPieces(p.torrentOffset(p.Pos), ra)
	return
}

func (p *PositionReader) torrentOffset(readerPos int64) int64 {
	return p.Offset + readerPos
}

// Returns the range of pieces [begin, end) that contains the extent of bytes.
func (p *PositionReader) byteRegionPieces(off, size int64) (begin, end int) {
	if off >= p.FileLength {
		return
	}
	if off < 0 {
		size += off
		off = 0
	}
	if size <= 0 {
		return
	}
	begin = int(off / p.PieceLength)
	end = int((off + size + p.PieceLength - 1) / p.PieceLength)
	if end > int(p.Pieces) {
		end = int(p.Pieces)
	}
	return
}
