package bittorrent

import (
	"time"
	"math/rand"

	gotorrent "github.com/anacrolix/torrent"
)

type Reader struct {
	*gotorrent.Reader
	*gotorrent.File
	*Torrent

	id int32
	closing chan struct{}
}

func (t *Torrent) NewReader(f *gotorrent.File) *Reader {
	rand.Seed(time.Now().UTC().UnixNano())

	reader := &Reader{
		Reader: t.Torrent.NewReader(),
		File:   f,
		Torrent: t,

		id: rand.Int31(),
		closing: make(chan struct{}),
	}

	go reader.Watch()
	return reader
}

func (r *Reader) Watch() {
	ticker := time.NewTicker(10 * time.Second)

	defer ticker.Stop()

	for {
		select {
		case <- ticker.C:
			log.Debugf("CurrentPos from Tick (%d): %d", r.id, r.Reader.CurrentPos())
			r.Torrent.CurrentPos(r.Reader.CurrentPos(), r.File)
			//tfsLog.Debugf("Current position for %d: %#v", r.id, r.Reader.CurrentPos())
		case <- r.closing:
			return
		}
	}
}

func (r *Reader) Close() error {
	log.Debugf("Closing reader: %#v", r.id)
	r.closing <- struct{}{}
	// defer close(r.closing)

	return r.Reader.Close()
}

// func (r *Reader) Seek(off int64, whence int) (int64, error) {
// 	log.Debugf("CurrentPos from Seek: %d", r.Reader.CurrentPos())
// 	r.Torrent.CurrentPos(r.Reader.CurrentPos(), r.File)
// 	return r.Reader.Seek(off, whence)
// }
