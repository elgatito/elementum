package storage

import (
	"io"
	"math"
	"sync"
	"time"
	"runtime"

	"github.com/anacrolix/missinggo/pubsub"
	"github.com/op/go-logging"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var log = logging.MustGetLogger("memory")

type memoryStorage struct {
	maxMemorySize int64

	readerEvents *pubsub.PubSub
	pc           *memoryPieceCompletion
}

type memoryTorrentStorage struct {
	bufContainer [][]byte
	bufAllocated map[int]int
	bufQueue     map[int]int
	bufSize      int

	mu      sync.Mutex
	pl      int64
	closing chan struct{}

	readerChanges *pubsub.Subscription
	s             *memoryStorage
	infoHash      metainfo.Hash
}

type memoryStoragePiece struct {
	index int

	ts *memoryTorrentStorage
	pc *memoryPieceCompletion
	mu sync.Mutex

	p  metainfo.Piece
	ih metainfo.Hash
}

type memoryPieceCompletion struct {
	mu sync.Mutex
	m  map[metainfo.PieceKey]struct{}
}

type StorageChange struct {
	InfoHash   string
	Pos        int64
	FileLength int64
	FileOffset int64
}

func NewMemoryStorage(maxMemorySize int64, readerEvents *pubsub.PubSub) *memoryStorage {
	return &memoryStorage{
		maxMemorySize: maxMemorySize,
		readerEvents:  readerEvents,
		pc:            NewMemoryPieceCompletion(),
	}
}

func (s *memoryStorage) OpenTorrent(info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	// Adding 1 reserved Piece space, 3MB to allow postbuffer storage
	postbufferSize := info.PieceLength
	for postbufferSize < 3*1024*1024 {
		postbufferSize += info.PieceLength
	}

	bufSize := int(math.Ceil(float64(s.maxMemorySize+postbufferSize+(2*info.PieceLength)) / float64(info.PieceLength)))
	buffers := make([][]byte, bufSize)
	for i := range buffers {
		// buffers[i] = make([]byte, 0, info.PieceLength)
		buffers[i] = make([]byte, info.PieceLength)
	}

	log.Debugf("Opening memory storage for %d pieces (%d limit, %d postbuffer)", bufSize, s.maxMemorySize, postbufferSize)

	// Forcing PieceCompletion cleanup to avoid caching
	s.pc = NewMemoryPieceCompletion()

	t := &memoryTorrentStorage{
		bufContainer: buffers,
		bufSize:      bufSize,

		bufAllocated: map[int]int{},
		bufQueue:     map[int]int{},

		readerChanges: s.readerEvents.Subscribe(),
		closing:       make(chan struct{}, 1),

		s:        s,
		pl:       info.PieceLength,
		infoHash: infoHash,
	}
	go t.Watch()

	return t, nil
}

func (s *memoryStorage) Close() error {
	return s.pc.Close()
}

func (ts *memoryTorrentStorage) Watch() {
	minute := time.NewTicker(1 * time.Minute)

	defer minute.Stop()
	defer close(ts.closing)
	defer ts.readerChanges.Close()

	var m runtime.MemStats

	for {
		select {
		case _i, ok := <-ts.readerChanges.Values:
			if !ok {
				continue
			}

			i := _i.(StorageChange)
			ts.UpdateBuffers(i)

		case <-ts.closing:
			runtime.ReadMemStats(&m)
			log.Debugf("Pre-Close Memory: %d, %d, %d, %d", m.HeapSys, m.HeapAlloc, m.HeapIdle, m.HeapReleased)

			ts.bufContainer = nil

			runtime.GC()

			runtime.ReadMemStats(&m)
			log.Debugf("Post-Close Memory: %d, %d, %d, %d", m.HeapSys, m.HeapAlloc, m.HeapIdle, m.HeapReleased)

			return

		case <- minute.C:
			runtime.ReadMemStats(&m)
			log.Debugf("Memory: %d, %d, %d, %d", m.HeapSys, m.HeapAlloc, m.HeapIdle, m.HeapReleased)
		}
	}
}

func (ts *memoryTorrentStorage) UpdateBuffers(sc StorageChange) {
	// log.Debugf("RF %v", sc)
	// if sc.Pos > sc.FileOffset+sc.FileLength-(5*1024*1024) {
	// 	return
	// }

	for index := range ts.bufAllocated {
		pieceOffset := ts.pl * int64(index)
		if pieceOffset < sc.Pos-(ts.pl*2) {
			ts.ResetBuffer(index)
		} else if pieceOffset > (sc.Pos + ts.s.maxMemorySize + ts.pl) {
			ts.ResetBuffer(index)
		}
	}
}

func (ts *memoryTorrentStorage) GetBuffer(index int, create bool) (int, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if i, ok := ts.bufAllocated[index]; ok {
		return i, true
	}

	if create {
		for i := 0; i < ts.bufSize; i++ {
			if _, ok := ts.bufQueue[i]; !ok {
				ts.bufAllocated[index] = i
				ts.bufQueue[i] = index
				log.Debugf("GET EMPTY %d (id: %#v): %#v, %#v", index, i, ts.bufQueue, ts.bufAllocated)
				return i, true
			}

		}

		log.Debugf("GET FAILED %d: Q:%#v", index, ts.bufQueue)
	}

	return -1, false
}

func (ts *memoryTorrentStorage) ResetBuffer(index int) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if i, ok := ts.bufAllocated[index]; ok {
		if _, ok := ts.bufQueue[i]; ok {
			log.Debugf("RB %d: Q:%v, A:%v", index, ts.bufQueue, ts.bufAllocated)
			delete(ts.bufQueue, i)
			delete(ts.bufAllocated, index)
			ts.bufContainer[i] = make([]byte, ts.pl)

			ts.s.pc.Set(metainfo.PieceKey{
				InfoHash: ts.infoHash,
				Index:    index,
			}, false)
			return true
		}
	}

	return false
}

func (ts *memoryTorrentStorage) Piece(p metainfo.Piece) storage.PieceImpl {
	return &memoryStoragePiece{
		index: p.Index(),
		pc:    ts.s.pc,
		p:     p,
		ts:    ts,
	}
}

func (ts *memoryTorrentStorage) Close() error {
	ts.closing <- struct{}{}
	return nil
}

func (me *memoryStoragePiece) pieceKey() metainfo.PieceKey {
	return metainfo.PieceKey{
		InfoHash: me.ih,
		Index:    me.p.Index(),
	}
}

func (sp *memoryStoragePiece) GetIsComplete() (ret bool) {
	ret, _ = sp.pc.Get(sp.pieceKey())
	return
}

func (sp *memoryStoragePiece) MarkComplete() error {
	sp.pc.Set(sp.pieceKey(), true)
	return nil
}

func (sp *memoryStoragePiece) MarkNotComplete() error {
	sp.pc.Set(sp.pieceKey(), false)
	return nil
}

func (sp *memoryStoragePiece) ReadAt(b []byte, off int64) (n int, err error) {
	bufIndex, ok := sp.ts.GetBuffer(sp.index, false)
	if !ok {
		// log.Debugf("Can't find buffer for read: %#v, %d, %#v", sp.p, off, bufIndex)
		// return 0, errors.New("Piece not ready yet")
		return 0, io.ErrUnexpectedEOF
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	// 	log.Debugf("Trying to read %d: %d (%d), BL: %#v", sp.index, off, sp.p.Length(), len(sp.ts.bufContainer[bufIndex]))

	// if off < 0 {
	// 	return 0, errors.New("bytes.Reader.ReadAt: negative offset")
	// }
	// if off >= int64(len(sp.ts.bufContainer[bufIndex])) {
	// 	return 0, io.EOF
	// }
	//readlen := sp.p.Length() - off
	// readlen := len(b)
	// n = copy(b, sp.ts.bufContainer[bufIndex][off:readlen])

	n1 := copy(b, sp.ts.bufContainer[bufIndex][off:])
	off = 0
	b = b[n1:]
	n += n1

	// n = copy(b, sp.ts.bufContainer[bufIndex][off:chunkSize])
	// if n < len(b) {
	// 	err = io.EOF
	// }
	// 	log.Debugf("Read off: %#v - %#v -- %#v (%#v)", off, chunkSize, sp.p.Length(), err)
	// 	// log.Debugf("Read off: [%#v:%#v] -- %#v -- len(%#v) -- (%#v)", off, readlen, sp.p.Length(), len(b), err)

	// reader := bufio.NewReader(sp.buf)
	// sp.buf.
	// b = []byte{}
	// b = append(b, sp.buf[:]...)
	// b = reader.

	// b = sp.buf[off:]
	// n = len(b)

	return
}

func (sp *memoryStoragePiece) WriteAt(b []byte, off int64) (n int, err error) {
	bufIndex, ok := sp.ts.GetBuffer(sp.index, true)
	if !ok {
		log.Debugf("Can't find buffer for write: %#v, %d, %#v", sp.p, off, bufIndex)
		return
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	// 	log.Debugf("TB %d: %d (%d), WL: %#v, BL: %#v", sp.index, off, sp.p.Length(), len(b), len(sp.ts.bufContainer[bufIndex]))

	n1 := copy(sp.ts.bufContainer[bufIndex][off:], b)
	b = b[n1:]
	off = 0
	n += n1

	// _n := len(b)
	// _off := int(off)
	// if _off == 0 {
	// 	sp.ts.bufContainer[bufIndex] = append(b[:], sp.ts.bufContainer[bufIndex][_n:]...)
	// } else if _n + _off + 1 >= len(sp.ts.bufContainer[bufIndex]) {
	// 	sp.ts.bufContainer[bufIndex] = append(sp.ts.bufContainer[bufIndex][:_off], b[:]...)
	// } else {
	// 	sp.ts.bufContainer[bufIndex] = append(sp.ts.bufContainer[bufIndex][:_off], append(b[:], sp.ts.bufContainer[bufIndex][_off+_n:]...)...)
	// }

	// 	log.Debugf("TA %d: %d (%d), WL: %#v, BL: %#v", sp.index, off, sp.p.Length(), len(b), len(sp.ts.bufContainer[bufIndex]))

	// n += _n
	// for sp.ts.isReady
	// 	<- sp.ts.ready
	// <- sp.ts.empty

	// sp.buf = append(sp.buf[:off+1], b...)
	// n = len(b)

	return
}

func NewMemoryPieceCompletion() *memoryPieceCompletion {
	return &memoryPieceCompletion{m: make(map[metainfo.PieceKey]struct{})}
}

func (*memoryPieceCompletion) Close() error { return nil }

func (me *memoryPieceCompletion) Get(pk metainfo.PieceKey) (bool, error) {
	me.mu.Lock()
	_, ok := me.m[pk]
	me.mu.Unlock()
	return ok, nil
}

func (me *memoryPieceCompletion) Set(pk metainfo.PieceKey, b bool) error {
	me.mu.Lock()
	if b {
		if me.m == nil {
			me.m = make(map[metainfo.PieceKey]struct{})
		}
		me.m[pk] = struct{}{}
	} else {
		delete(me.m, pk)
	}
	log.Debugf("SET COMPLETE: %#v, %#v", b, pk.Index)
	me.mu.Unlock()
	return nil
}
