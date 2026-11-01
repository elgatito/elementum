package mdblist

import (
	"testing"
	"time"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/tracker"
)

func TestListEntryIDs(t *testing.T) {
	// Shapes from the MDBList list items example: movies carry ids, shows only flat fields
	var page struct {
		Movies []*listEntry `json:"movies"`
		Shows  []*listEntry `json:"shows"`
	}
	if err := json.Unmarshal([]byte(`{
		"movies": [{"id": 917496, "title": "Beetlejuice Beetlejuice", "imdb_id": "tt2049403", "tvdb_id": null, "release_year": 2024,
			"ids": {"mdblist": "m917496", "imdb": "tt2049403", "tmdb": 917496, "tvdb": null}}],
		"shows": [{"id": 258902, "title": "English Teacher", "imdb_id": "tt20782190", "tvdb_id": 421968, "release_year": 2024}]
	}`), &page); err != nil {
		t.Fatal(err)
	}

	m := page.Movies[0].item(tracker.MovieType)
	if m.IDs.TMDB != 917496 || m.IDs.Native != "m917496" || m.Year != 2024 {
		t.Errorf("bad movie: %+v", m)
	}
	s := page.Shows[0].item(tracker.ShowType)
	if s.IDs.TMDB != 258902 || s.IDs.IMDB != "tt20782190" || s.IDs.TVDB != 421968 {
		t.Errorf("show must fall back to flat ids: %+v", s)
	}
}

func TestWatchedBody(t *testing.T) {
	day := time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)
	body := watchedBody([]*tracker.WatchedItem{
		{Item: tracker.Item{Type: tracker.MovieType, IDs: tracker.IDs{TMDB: 272}}, LastWatchedAt: day},
		{Item: tracker.Item{Type: tracker.EpisodeType, IDs: tracker.IDs{TMDB: 1396}, Season: 1, Episode: 3}},
		{Item: tracker.Item{Type: tracker.EpisodeType, IDs: tracker.IDs{TMDB: 1396}, Season: 1, Episode: 4}},
		{Item: tracker.Item{Type: tracker.SeasonType, IDs: tracker.IDs{TMDB: 1396}, Season: 2}},
	})

	b, _ := json.Marshal(body)
	want := `{"movies":[{"ids":{"tmdb":272},"watched_at":"2025-10-21T12:00:00Z"}],` +
		`"shows":[{"ids":{"tmdb":1396},"seasons":[{"number":1,"episodes":[{"number":3},{"number":4}]},{"number":2}]}]}`
	if string(b) != want {
		t.Errorf("watchedBody =\n%s\nwant\n%s", b, want)
	}
}

func TestSyncRowWatchedAt(t *testing.T) {
	var r syncRow
	if err := json.Unmarshal([]byte(`{"watched_at": null, "last_watched_at": "2026-10-17T21:30:00.000Z"}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.watchedAt().IsZero() {
		t.Error("last_watched_at must be used when watched_at is null")
	}
}
