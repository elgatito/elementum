package bittorrent

import (
	"os"
	"time"
	"net/http"
	"path/filepath"

	"github.com/op/go-logging"
)

type TorrentFS struct {
	http.Dir
	service *BTService
}

var tfsLog = logging.MustGetLogger("torrentfs")

func TorrentFSHandler(btService *BTService, downloadPath string)  http.Handler {
	return http.StripPrefix("/files", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entry, err := NewTorrentFS(btService, downloadPath).
								 Open(r.URL.Path)

		if err == nil && entry != nil {
			defer entry.Close()
			// http.ServeContent(w, r, file.File.DisplayPath(), time.Now(), file)
			http.ServeContent(w, r, entry.File.DisplayPath(), time.Now(), entry.rs)
		} else {
			tfsLog.Noticef("Could not find torrent for requested file %s: %#v", r.URL.Path, err)
		}
	}))
}

func NewTorrentFS(service *BTService, path string) *TorrentFS {
	return &TorrentFS{
		service: service,
		Dir:     http.Dir(path),
	}
}

func (tfs *TorrentFS) Open(name string) (*FileEntry, error) {
	file, err := os.Open(filepath.Join(string(tfs.Dir), name))
	if err != nil {
		return nil, err
	}
	// make sure we don't open a file that's locked, as it can happen
	// on BSD systems (darwin included)
	if unlockerr := unlockFile(file); unlockerr != nil {
		tfsLog.Errorf("Unable to unlock file because: %s", unlockerr)
	}

	tfsLog.Infof("Opening %s", name)
	for _, torrent := range tfs.service.Torrents {
		for _, f := range torrent.Files() {
			if name[1:] == f.Path() {
				tfsLog.Noticef("%s belongs to torrent %s", name, torrent.Name())
				if entry, createerr := NewFileReader(torrent, &f, !torrent.IsRarArchive); createerr == nil {
					torrent.GetDBID()
					return entry, nil
				}
			}
		}
	}

	return nil, err
}
