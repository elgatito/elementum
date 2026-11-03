package scrob

import (
	"testing"
	"time"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/tracker"
)

func TestEventShapes(t *testing.T) {
	// Shapes from Scrob history and list responses: naive UTC timestamps, nulls, show fields on episodes
	var res struct {
		Results []*event    `json:"results"`
		Items   []*listItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(`{
		"results": [
			{"media": {"id": 1, "tmdb_id": 1399001, "tvdb_id": null, "imdb_id": null, "type": "episode", "title": "Winter Is Coming",
				"release_date": "2011-04-17", "season_number": 1, "episode_number": 1, "runtime": 62,
				"show_title": "Game of Thrones", "show_tmdb_id": 1399, "show_tvdb_id": 121361},
			 "watched_at": "2026-11-01T20:15:30.123456", "progress_percent": 0.42, "play_count": 1},
			{"media": {"tmdb_id": 603, "type": "movie", "title": "The Matrix", "release_date": "1999-03-31"},
			 "watched_at": null}
		],
		"items": [
			{"id": 7, "added_at": "2026-10-01T10:00:00+00:00", "media": {"tmdb_id": 1399, "type": "series", "title": "Game of Thrones", "season_number": 2}},
			{"id": 8, "added_at": "2026-10-02T10:00:00", "media": {"tmdb_id": 1399, "type": "series", "title": "Game of Thrones", "season_number": null}}
		]
	}`), &res); err != nil {
		t.Fatal(err)
	}

	ep := res.Results[0]
	if want := time.Date(2026, 11, 1, 20, 15, 30, 123456000, time.UTC); !ep.WatchedAt.Equal(want) {
		t.Errorf("watched_at = %v, want %v", ep.WatchedAt, want)
	}
	i := ep.Media.item()
	if i.Type != tracker.EpisodeType || i.IDs.TMDB != 1399 || i.IDs.TVDB != 121361 || i.EpisodeTMDB != 1399001 || i.Title != "Game of Thrones" || i.Season != 1 || i.Episode != 1 {
		t.Errorf("bad episode: %+v", i)
	}
	if s := ep.Media.show(); s.Type != tracker.ShowType || s.Key() != (&tracker.Item{Type: tracker.ShowType, IDs: tracker.IDs{TMDB: 1399}}).Key() {
		t.Errorf("bad show: %+v", s)
	}

	movie := res.Results[1]
	if m := movie.Media.item(); m.Type != tracker.MovieType || m.IDs.TMDB != 603 || m.Year != 1999 || !movie.WatchedAt.IsZero() {
		t.Errorf("bad movie: %+v, watched %v", m, movie.WatchedAt)
	}

	season, show := res.Items[0].Media.item(), res.Items[1].Media.item()
	if season.Type != tracker.SeasonType || season.Season != 2 || show.Type != tracker.ShowType {
		t.Errorf("bad list items: %+v %+v", season, show)
	}
	// list removal matches entries by the key of what an item becomes in a list
	if listTarget(&tracker.Item{Type: tracker.EpisodeType, IDs: tracker.IDs{TMDB: 1399}, Season: 3, Episode: 4}).Key() != show.Key() {
		t.Error("an episode should target its show in lists")
	}
	if listTarget(&tracker.Item{Type: tracker.SeasonType, IDs: tracker.IDs{TMDB: 1399}, Season: 2}).Key() != season.Key() {
		t.Error("a season should target the season entry in lists")
	}
}
