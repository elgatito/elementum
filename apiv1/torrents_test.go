package apiv1

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/tmdb"
)

func TestToTorrent(t *testing.T) {
	added := time.Unix(1700000000, 0)
	got := toTorrent(torrentInfo{
		hash: "08ADA5A7A6183AAE1E09D831DF6748D566095A10", name: "Sintel", size: 100, percent: 150, state: bittorrent.StatusSeeding,
		down: 1, up: 2, seeds: 3, peers: 4, added: added, players: 1,
		db: &database.BTItem{Type: "episode", ID: 7, ShowID: 42, Season: 2, Episode: 5},
	})
	if got.InfoHash != "08ada5a7a6183aae1e09d831df6748d566095a10" || got.State != TorrentStateSeeding || got.Progress != 1 ||
		got.AddedAt != 1700000000 || !got.HasPlayer || *got.TmdbId != 42 || *got.MediaType != TorrentMediaTypeEpisode || *got.Season != 2 || *got.Episode != 5 {
		t.Fatalf("%+v", got)
	}
	m := toTorrent(torrentInfo{state: bittorrent.StatusPaused, paused: true, db: &database.BTItem{Type: "movie", ID: 45745}})
	if m.State != TorrentStatePaused || !m.Paused || *m.TmdbId != 45745 || *m.MediaType != TorrentMediaTypeMovie || m.Season != nil || m.HasPlayer {
		t.Fatalf("%+v", m)
	}
	if u := toTorrent(torrentInfo{state: 99}); u.TmdbId != nil || u.MediaType != nil || u.State != TorrentStateQueued {
		t.Fatalf("%+v", u)
	}
}

func TestTorrentStatesMatchStatusConstants(t *testing.T) {
	want := map[int]TorrentState{
		bittorrent.StatusQueued: TorrentStateQueued, bittorrent.StatusChecking: TorrentStateChecking, bittorrent.StatusFinding: TorrentStateFinding,
		bittorrent.StatusDownloading: TorrentStateDownloading, bittorrent.StatusFinished: TorrentStateFinished, bittorrent.StatusSeeding: TorrentStateSeeding,
		bittorrent.StatusAllocating: TorrentStateAllocating, bittorrent.StatusStalled: TorrentStateStalled, bittorrent.StatusPaused: TorrentStatePaused,
		bittorrent.StatusBuffering: TorrentStateBuffering, bittorrent.StatusPlaying: TorrentStatePlaying,
	}
	if len(want) != len(bittorrent.StatusStrings) || len(torrentStates) != len(want) {
		t.Fatalf("%d states, %d statuses", len(torrentStates), len(bittorrent.StatusStrings))
	}
	for code, state := range want {
		if toTorrent(torrentInfo{state: code}).State != state {
			t.Errorf("status %d is not %s", code, state)
		}
	}
}

func TestToTorrentUnknownIDsAndName(t *testing.T) {
	got := toTorrent(torrentInfo{hash: "ABCD", db: &database.BTItem{Type: "movie", ID: 0}})
	if got.Name != "abcd" || got.TmdbId != nil || got.MediaType != nil {
		t.Fatalf("%+v", got)
	}
	ep := toTorrent(torrentInfo{db: &database.BTItem{Type: "episode", ShowID: 3, Season: 0}})
	if *ep.TmdbId != 3 || *ep.Season != 0 || ep.Episode != nil {
		t.Fatalf("%+v", ep)
	}
}

func TestBadInfoHash(t *testing.T) {
	for _, h := range []string{"", "abc", "zz" + string(make([]byte, 38)), "08ada5a7a6183aae1e09d831df6748d566095a1"} {
		if _, e := findTorrent(h); e != &badHash {
			t.Errorf("%q: %v", h, e)
		}
	}
	s := Server{}
	if r, _ := s.PauseTorrent(context.Background(), PauseTorrentRequestObject{InfoHash: "x"}); r.(PauseTorrent400JSONResponse).BadRequestJSONResponse.Error.Code != ErrorCodeBadRequest {
		t.Error("pause")
	}
	if _, ok := mustRemove(s, "x").(RemoveTorrent400JSONResponse); !ok {
		t.Error("remove")
	}
}

func mustRemove(s Server, h string) RemoveTorrentResponseObject {
	r, _ := s.RemoveTorrent(context.Background(), RemoveTorrentRequestObject{InfoHash: h})
	return r
}

func TestStopSessions(t *testing.T) {
	sessions.Lock()
	a, b := &session{id: "a", hash: "h1", state: PlaybackStateReady}, &session{id: "b", hash: "h2", state: PlaybackStateReady}
	sessions.byID = map[string]*session{"a": a, "b": b}
	sessions.active = a
	sessions.Unlock()
	stopSessions(func(s *session) bool { return s.hash == "h1" })
	sessions.Lock()
	defer sessions.Unlock()
	if len(sessions.byID) != 1 || sessions.byID["b"] != b || a.state != PlaybackStateStopped || sessions.active != nil || b.state != PlaybackStateReady {
		t.Fatalf("%v %v", sessions.byID, a.state)
	}
	sessions.byID = map[string]*session{}
}

func TestToPerson(t *testing.T) {
	b, _ := os.ReadFile("testdata/person.json")
	var p tmdb.Person
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	got := toPerson(&p)
	if got.TmdbId != 6384 || got.Name != "Keanu Reeves" || *got.Profile != tmdb.ImageURL(p.ProfilePath, "w185") || got.Birthday.Format("2006-01-02") != "1964-09-02" ||
		got.Deathday != nil || got.Biography != "" || *got.PlaceOfBirth != "Beirut, Lebanon" || *got.KnownForDepartment != "Acting" {
		t.Fatalf("%+v", got)
	}
}

func TestPersonBadLang(t *testing.T) {
	bad := "english"
	r, _ := Server{}.GetPerson(context.Background(), GetPersonRequestObject{TmdbId: 1, Params: GetPersonParams{Lang: &bad}})
	if _, ok := r.(GetPerson400JSONResponse); !ok {
		t.Fatalf("%T", r)
	}
	r2, _ := Server{}.GetPersonCredits(context.Background(), GetPersonCreditsRequestObject{TmdbId: 1, Params: GetPersonCreditsParams{Lang: &bad}})
	if _, ok := r2.(GetPersonCredits400JSONResponse); !ok {
		t.Fatalf("%T", r2)
	}
}
