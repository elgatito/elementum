package apiv1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tmdb"
)

// load decodes a TMDB response fixture, as fetched with language=de-DE.
func load[T any](t *testing.T, name string) *T {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return &v
}

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

func eq[T any](t *testing.T, field string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %#v, want %#v", field, got, want)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestMovieDetails(t *testing.T) {
	m := movieDetails(load[tmdb.Movie](t, "movie_603_de-DE.json"))

	eq(t, "type", m.Type, MediaTypeMovie)
	eq(t, "tmdb_id", m.TmdbId, 603)
	eq(t, "title", m.Title, "Matrix") // German title, not the original
	eq(t, "original_title", m.OriginalTitle, "The Matrix")
	eq(t, "overview", m.Overview, "Der Hacker Neo wird übers Internet von einer geheimnisvollen Untergrund-Organisation kontaktiert.")
	eq(t, "tagline", m.Tagline, "Willkommen in der realen Welt.")
	eq(t, "year", m.Year, num(1999))
	eq(t, "release_date", m.ReleaseDate.String(), "1999-03-31")
	eq(t, "runtime", m.Runtime, num(136))
	eq(t, "rating", m.Rating, float32(8.2))
	eq(t, "genres", m.Genres, []string{"Action", "Science Fiction"})
	eq(t, "poster", m.Poster, str("https://image.tmdb.org/t/p/w342/9X9KrQ4oQUvRkA5JY6mBIN7uSNb.jpg"))
	eq(t, "backdrop", m.Backdrop, str("https://image.tmdb.org/t/p/w1280/fNG7i7RqMErkcqhohV2a6cV1Ehy.jpg"))

	eq(t, "cast", m.Cast, []CastMember{
		{TmdbId: 6384, Name: "Keanu Reeves", Character: "Thomas A. Anderson / Neo", Profile: str("https://image.tmdb.org/t/p/w185/4D0PpNI0kmP58hgrwGC3wCjxhnm.jpg")},
		{TmdbId: 2975, Name: "Laurence Fishburne", Character: "Morpheus"},
	})
	eq(t, "crew", m.Crew, []CrewMember{{TmdbId: 9340, Name: "Lana Wachowski", Job: "Director", Department: "Directing"}})
	eq(t, "images", m.Images, Images{
		Posters:   []Image{{Url: "https://image.tmdb.org/t/p/w342/9X9KrQ4oQUvRkA5JY6mBIN7uSNb.jpg", Width: 2000, Height: 3000, Language: str("de")}},
		Backdrops: []Image{{Url: "https://image.tmdb.org/t/p/w1280/icmmSD4vTTDKOq2vvdulafOGw93.jpg", Width: 3840, Height: 2160}},
		Logos:     []Image{{Url: "https://image.tmdb.org/t/p/original/vzAVHkM7Szc4qOYdMXr1OmCVEuy.png", Width: 1500, Height: 359, Language: str("en")}},
	})
	eq(t, "videos", m.Videos, []Video{{Name: "Matrix - Trailer (Deutsch)", Site: "YouTube", Key: "vKQi3bBA1y8", Type: "Trailer"}})
	eq(t, "imdb_id", deref(m.ExternalIds.ImdbId), any("tt0133093"))
	eq(t, "tvdb_id", m.ExternalIds.TvdbId, nil)
}

func TestShowDetails(t *testing.T) {
	s := showDetails(load[tmdb.Show](t, "show_1396_de-DE.json"))

	eq(t, "type", s.Type, MediaTypeShow)
	eq(t, "title", s.Title, "Breaking Bad")
	// No German overview on TMDB: falls back to the English translation.
	eq(t, "overview", s.Overview, "Walter White, a New Mexico chemistry teacher, is diagnosed with Stage III cancer.")
	eq(t, "year", s.Year, num(2008))
	eq(t, "first_air_date", s.FirstAirDate.String(), "2008-01-20")
	eq(t, "genres", s.Genres, []string{"Drama", "Krimi"})
	eq(t, "status", s.Status, "Ended")
	eq(t, "episode_runtime", s.EpisodeRuntime, num(56)) // episode_run_time is empty: from last_episode_to_air
	eq(t, "number_of_seasons", s.NumberOfSeasons, 5)
	eq(t, "number_of_episodes", s.NumberOfEpisodes, 62)
	eq(t, "tvdb_id", s.ExternalIds.TvdbId, num(81189))
	eq(t, "imdb_id", deref(s.ExternalIds.ImdbId), any("tt0903747"))
	eq(t, "cast", s.Cast, []CastMember{})
	eq(t, "images", s.Images, Images{Posters: []Image{}, Backdrops: []Image{}, Logos: []Image{}})
	eq(t, "videos", s.Videos, []Video{})
	eq(t, "seasons", len(s.Seasons), 2)
	eq(t, "season 0 poster", s.Seasons[0].Poster, nil)
	eq(t, "season 1", s.Seasons[1].Name, "Staffel 1")
	eq(t, "season 1 episodes", s.Seasons[1].EpisodeCount, 7)
	eq(t, "season 1 poster", s.Seasons[1].Poster, str("https://image.tmdb.org/t/p/w342/1BP4xYv9ZG4ZVHkL7ocOziBbSYH.jpg"))
}

func TestSeason(t *testing.T) {
	s := season(load[tmdb.Season](t, "season_1396_1_de-DE.json"))

	eq(t, "season_number", s.SeasonNumber, 1)
	eq(t, "name", s.Name, "Staffel 1")
	eq(t, "air_date", s.AirDate.String(), "2008-01-20")
	eq(t, "episodes", len(s.Episodes), 2)
	e1, e2 := s.Episodes[0], s.Episodes[1]
	eq(t, "e1", []any{e1.TmdbId, e1.SeasonNumber, e1.EpisodeNumber, e1.Name, e1.Rating}, []any{62085, 1, 1, "Der Einstieg", float32(8.3)})
	eq(t, "e1 still", e1.Still, str("https://image.tmdb.org/t/p/w300/ydlY3iPfeOAvu8gVqrxPoMvzNCn.jpg"))
	eq(t, "e1 runtime", e1.Runtime, num(58))
	eq(t, "e2 air_date", e2.AirDate, nil)
	eq(t, "e2 still", e2.Still, nil)
	eq(t, "e2 runtime", e2.Runtime, nil)
}

func TestItemsSkipNil(t *testing.T) {
	m := load[tmdb.Movie](t, "movie_603_de-DE.json")
	s := load[tmdb.Show](t, "show_1396_de-DE.json")
	eq(t, "movies", movieItems(tmdb.Movies{nil, m, nil}), []MediaItem{movieItem(m)})
	eq(t, "shows", showItems(tmdb.Shows{s, nil}), []MediaItem{showItem(s)})
}

// Array bodies must encode as bare arrays, also when empty (see the note on responses in openapi.yaml).
func TestArrayResponses(t *testing.T) {
	for name, tc := range map[string]struct {
		visit func(http.ResponseWriter) error
		want  string
	}{
		"genres":        {ListMovieGenres200JSONResponse(genres([]*tmdb.Genre{{ID: 28, Name: "Action"}, nil})).VisitListMovieGenresResponse, `[{"id":28,"name":"Action"}]`},
		"show genres":   {ListShowGenres200JSONResponse(genres(nil)).VisitListShowGenresResponse, `[]`},
		"streams":       {ListMovieStreams200JSONResponse(toStreams([]*bittorrent.TorrentFile{{URI: "magnet:?xt=urn:btih:abc"}})).VisitListMovieStreamsResponse, `[{"audio":null,`},
		"no streams":    {ListMovieStreams200JSONResponse(toStreams(nil)).VisitListMovieStreamsResponse, `[]`},
		"client gone":   {ListEpisodeStreams200JSONResponse{}.VisitListEpisodeStreamsResponse, `[]`},
		"tracker lists": {ListTrackerLists200JSONResponse{}.VisitListTrackerListsResponse, `[]`},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			if err := tc.visit(w); err != nil {
				t.Fatal(err)
			}
			body := w.Body.String()
			// a want not ending in "]" is a prefix of a longer array
			if ok := body == tc.want+"\n" || !strings.HasSuffix(tc.want, "]") && strings.HasPrefix(body, tc.want) && strings.HasSuffix(body, "]\n"); !ok {
				t.Errorf("body %q, want %q", body, tc.want)
			}
		})
	}
}

func TestMediaPage(t *testing.T) {
	for _, tc := range []struct{ total, limit, pages int }{
		{0, 20, 0},
		{1, 20, 1},
		{40, 20, 2},
		{41, 20, 3},
		{41, 7, 6},
		{999999, 20, 500}, // TMDB serves at most 500 pages of 20
		{999999, 30, 334},
	} {
		p := mediaPage(nil, tc.total, 2, tc.limit)
		if p.TotalPages != tc.pages || *p.TotalResults != tc.total || p.Limit != tc.limit || p.Page != 2 {
			t.Errorf("total %d limit %d: %+v, want %d pages", tc.total, tc.limit, p, tc.pages)
		}
	}
}

func TestPastLastPageMath(t *testing.T) {
	for _, tc := range []struct {
		page, limit int
		past        bool
	}{{500, 20, false}, {501, 20, true}, {334, 30, false}, {335, 30, true}, {100, 100, false}, {101, 100, true}, {1 << 62, 7, true}} {
		if got := pastLastPage(tc.page, tc.limit); got != tc.past {
			t.Errorf("page %d limit %d: %v", tc.page, tc.limit, got)
		}
	}
}

func TestArgs(t *testing.T) {
	old := config.Get().Language
	t.Cleanup(func() { config.Get().Language = old })
	config.Get().Language = "fr"

	for in, want := range map[string]string{"de": "de", "DE": "de", "de-de": "de-DE", "pt_BR": "pt-BR"} {
		if _, _, got, e := args(nil, nil, &in); e != nil || got != want {
			t.Errorf("lang %q: %q, %v; want %q", in, got, e, want)
		}
	}
	for _, in := range []string{"", "deu", "de-", "de-DEU", "d1"} {
		if _, _, _, e := args(nil, nil, &in); e == nil || e.Error.Code != ErrorCodeBadRequest {
			t.Errorf("lang %q: %v, want bad_request", in, e)
		}
	}
	oldPerPage := config.Get().ResultsPerPage
	t.Cleanup(func() { config.Get().ResultsPerPage = oldPerPage })
	for perPage, want := range map[int]int{0: 20, -5: 1, 30: 30, 500: 100} {
		config.Get().ResultsPerPage = perPage
		if _, limit, _, _ := args(nil, nil, nil); limit != want {
			t.Errorf("results_per_page %d: limit %d, want %d", perPage, limit, want)
		}
	}
	page, limit, lang, e := args(nil, nil, nil)
	if e != nil || page != 1 || limit < 1 || lang != "fr" {
		t.Errorf("defaults: %d %d %q %v", page, limit, lang, e)
	}
	if page, limit, _, _ := args(num(3), num(50), nil); page != 3 || limit != 50 {
		t.Errorf("explicit: %d %d", page, limit)
	}
}
