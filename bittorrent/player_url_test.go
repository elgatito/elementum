package bittorrent

import (
	"testing"

	"github.com/elgatito/elementum/xbmc"
)

func TestIsFileURL(t *testing.T) {
	own := "Big%20Buck%20Bunny/Big%20Buck%20Bunny.mp4"
	if !isFileURL("http://127.0.0.1:65220/files/Big%20Buck%20Bunny/Big%20Buck%20Bunny.mp4", own) {
		t.Error("same file not matched")
	}
	if !isFileURL("http://192.168.1.2:65220/files/Big Buck Bunny/Big Buck Bunny.mp4", own) {
		t.Error("differently encoded same file not matched")
	}
	if isFileURL("http://127.0.0.1:65220/files/Sintel/Sintel.mp4", own) {
		t.Error("other file matched")
	}
	if isFileURL("/storage/videos/movie.mkv", own) {
		t.Error("non-Elementum file matched")
	}
}

func TestSameKodiItem(t *testing.T) {
	movie := &xbmc.PlayerItemInfo{}
	movie.Info.ID, movie.Info.Type = 4, "movie"
	same := &xbmc.PlayerItemInfo{}
	same.Info.ID, same.Info.Type = 4, "movie"
	episode := &xbmc.PlayerItemInfo{}
	episode.Info.ID, episode.Info.Type = 4, "episode"

	if !sameKodiItem(same, movie) {
		t.Error("same library item not matched")
	}
	if sameKodiItem(episode, movie) {
		t.Error("episode with the same id matched a movie")
	}
	if sameKodiItem(nil, movie) {
		t.Error("RPC error matched")
	}
}
