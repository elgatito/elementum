package bittorrent

import (
	"time"
	"math/rand"

	gotorrent "github.com/anacrolix/torrent"
)

type Reader struct {
	*gotorrent.Reader
	*gotorrent.File

	id int32
	closing chan struct{}
}

func (t *Torrent) NewReader(f *gotorrent.File) *Reader {
	rand.Seed(time.Now().UTC().UnixNano())
	reader := &Reader{
		Reader: t.Torrent.NewReader(),
		File:   f,

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
			tfsLog.Debugf("Current position for %d: %#v", r.id, r.Reader.CurrentPos())
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

// func (r *Reader) Seek(off int64, whence int) (ret int64, err error) {
// 	log.Debugf("Seek s %d: %#v -- %#v, %#v", r.id, r.Reader.CurrentPos(), off, whence)
// 	ret, err = r.Reader.Seek(off, whence)
// 	log.Debugf("Seek f %d: %#v -- %#v, %#v", r.id, r.Reader.CurrentPos(), ret, err)
// 	return
// }
