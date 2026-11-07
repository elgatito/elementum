package apiv1_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elgatito/elementum/apiv1"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"
)

// TestMain opens real cache and library DBs: cached TMDB/Trakt requests, tracker activities and the library need them.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "apiv1-test")
	if err != nil {
		panic(err)
	}
	conf := &config.Configuration{Info: &xbmc.AddonInfo{Profile: dir}}
	if _, err := database.InitCacheDB(conf); err != nil {
		panic(err)
	}
	if _, err := database.InitStormDB(conf); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// stubAPI points an API at a fresh server for this test and logs the requests it gets.
func stubAPI(t *testing.T, api *reqapi.API, h func(w http.ResponseWriter, r *http.Request)) func() []string {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path+"?page="+r.URL.Query().Get("page")+"&limit="+r.URL.Query().Get("limit"))
		mu.Unlock()
		h(w, r)
	}))
	old := api.Endpoint
	api.Endpoint = srv.URL
	t.Cleanup(func() { api.Endpoint = old; srv.Close() })
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) }
}

// stubTMDB serves /movie/<id> and /tv/<id> as "m<id>"/"s<id>", episodes as "e<number>", and finds tt0000012 as movie 12.
func stubTMDB(t *testing.T) {
	stubAPI(t, reqapi.TMDBAPI, func(w http.ResponseWriter, r *http.Request) {
		p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.URL.Path == "/find/tt0000012":
			fmt.Fprint(w, `{"movie_results": [{"id": 12}]}`)
		case len(p) == 2 && p[0] == "movie":
			fmt.Fprintf(w, `{"id": %s, "title": "m%s"}`, p[1], p[1])
		case len(p) == 2 && p[0] == "tv":
			fmt.Fprintf(w, `{"id": %s, "name": "s%s"}`, p[1], p[1])
		case len(p) == 6 && p[0] == "tv" && p[4] == "episode":
			fmt.Fprintf(w, `{"id": 9, "name": "e%s", "air_date": "2009-04-05", "still_path": "/x.jpg"}`, p[5])
		default:
			http.NotFound(w, r)
		}
	})
}

func getPage(t *testing.T, path string) apiv1.MediaItemPage {
	t.Helper()
	w := do(newEngine(), request{path: path})
	var page apiv1.MediaItemPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != http.StatusOK {
		t.Fatalf("%s: status %d, %v: %s", path, w.Code, err, w.Body)
	}
	return page
}

func titles(items []apiv1.MediaItem) (out []string) {
	for _, i := range items {
		out = append(out, fmt.Sprintf("%d:%s", i.TmdbId, i.Title))
	}
	return
}

func TestTraktChart(t *testing.T) {
	setCreds(t, "", "")
	stubTMDB(t)
	seen := stubAPI(t, reqapi.TraktAPI, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movies/trending" {
			http.NotFound(w, r)
			return
		}
		var page, limit int
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		fmt.Sscan(r.URL.Query().Get("limit"), &limit)
		items := []string{}
		for i := range limit {
			items = append(items, fmt.Sprintf(`{"watchers": 1, "movie": {"title": "t", "ids": {"tmdb": %d}}}`, page*100+i))
		}
		w.Header().Set("X-Pagination-Item-Count", "30")
		fmt.Fprintf(w, "[%s]", strings.Join(items, ","))
	})

	p := getPage(t, "/api/v1/trakt/movies/lists/trending?page=2&limit=7")
	if len(p.Items) != 7 || p.Items[0].TmdbId != 200 || p.Items[0].Title != "m200" || p.Items[0].Type != apiv1.MediaTypeMovie {
		t.Errorf("items = %v", titles(p.Items))
	}
	if p.Page != 2 || p.Limit != 7 || p.TotalPages != 5 || p.TotalResults == nil || *p.TotalResults != 30 {
		t.Errorf("page = %+v", p)
	}
	// Another limit must not be served from the first one's cache entry; the same one must.
	getPage(t, "/api/v1/trakt/movies/lists/trending?page=2&limit=5")
	getPage(t, "/api/v1/trakt/movies/lists/trending?page=2&limit=7")
	if got, want := seen(), []string{"/movies/trending?page=2&limit=7", "/movies/trending?page=2&limit=5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Trakt requests = %v, want %v", got, want)
	}

	if w := do(newEngine(), request{path: "/api/v1/trakt/shows/lists/popular"}); w.Code != http.StatusBadGateway {
		t.Errorf("failed Trakt request: status %d: %s", w.Code, w.Body)
	} else if strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Errorf("upstream error leaked: %s", w.Body)
	}
}

// fakeTracker has a watchlist and progress, but no collection or custom lists.
type fakeTracker struct{ authorized bool }

func (fakeTracker) ID() string                     { return "fake" }
func (fakeTracker) Name() string                   { return "Fake" }
func (fakeTracker) MinSyncInterval() time.Duration { return time.Minute }
func (f fakeTracker) Authorized() bool             { return f.authorized }
func (fakeTracker) Authorize() error               { return errors.New("must not be called") }
func (fakeTracker) Deauthorize() error             { return nil }
func (fakeTracker) RefreshToken() error            { return nil }
func (fakeTracker) LastActivities() (*tracker.Activities, error) {
	return nil, errors.New("offline")
}
func (fakeTracker) Watchlist(t tracker.MediaType, _ bool) ([]*tracker.Item, error) {
	if t == tracker.ShowType {
		return nil, errors.New("upstream https://api.example/?key=secret")
	}
	return []*tracker.Item{
		{IDs: tracker.IDs{TMDB: 11}}, {IDs: tracker.IDs{IMDB: "tt0000012"}}, {IDs: tracker.IDs{TMDB: 13}},
		{IDs: tracker.IDs{TMDB: 14}}, {IDs: tracker.IDs{TMDB: 15}},
	}, nil
}
func (fakeTracker) Watched(tracker.MediaType, bool) ([]*tracker.WatchedItem, error) { return nil, nil }
func (fakeTracker) Add(tracker.Location, string, []*tracker.Item) error             { return nil }
func (fakeTracker) Remove(tracker.Location, string, []*tracker.Item) error          { return nil }
func (fakeTracker) SetWatched([]*tracker.WatchedItem, bool) error                   { return nil }
func (fakeTracker) Scrobble(tracker.ScrobbleAction, *tracker.Item, float64) error   { return nil }
func (fakeTracker) Progress(bool) ([]*tracker.Item, error) {
	return []*tracker.Item{{Type: tracker.EpisodeType, IDs: tracker.IDs{TMDB: 1396}, Season: 2, Episode: 5}}, nil
}

func useTracker(t *testing.T, p tracker.Provider) {
	tracker.Register(p)
	old := config.Get().TrackerProvider
	t.Cleanup(func() { config.Get().TrackerProvider = old })
	config.Get().TrackerProvider = p.ID()
}

func TestTracker(t *testing.T) {
	setCreds(t, "", "")
	stubTMDB(t)
	useTracker(t, fakeTracker{authorized: true})

	p := getPage(t, "/api/v1/tracker/movies/watchlist?page=1&limit=2")
	if got := titles(p.Items); !reflect.DeepEqual(got, []string{"11:m11", "12:m12"}) { // 12 resolved by IMDB id
		t.Errorf("page 1 = %v", got)
	}
	if p.TotalPages != 3 || p.TotalResults == nil || *p.TotalResults != 5 {
		t.Errorf("page 1 = %+v", p)
	}
	if got := titles(getPage(t, "/api/v1/tracker/movies/watchlist?page=3&limit=2").Items); !reflect.DeepEqual(got, []string{"15:m15"}) {
		t.Errorf("page 3 = %v", got)
	}
	if p := getPage(t, "/api/v1/tracker/movies/watchlist?page=9223372036854775807&limit=2"); p.Items == nil || len(p.Items) != 0 {
		t.Errorf("past the end = %+v", p)
	}

	w := do(newEngine(), request{path: "/api/v1/tracker/shows/progress?lang=de-DE"})
	var np apiv1.NextEpisodePage
	if err := json.Unmarshal(w.Body.Bytes(), &np); err != nil || w.Code != http.StatusOK || len(np.Items) != 1 {
		t.Fatalf("progress: status %d, %v: %s", w.Code, err, w.Body)
	}
	if e := np.Items[0]; e.Show.TmdbId != 1396 || e.Show.Title != "s1396" || e.Season != 2 || e.Episode != 5 ||
		e.Title == nil || *e.Title != "e5" || e.AirDate == nil || e.Still == nil {
		t.Errorf("progress item = %+v", e)
	}

	for path, want := range map[string]struct {
		status int
		code   apiv1.ErrorCode
	}{
		"/api/v1/tracker/movies/collection":                       {http.StatusConflict, apiv1.ErrorCodeTrackerUnsupported},
		"/api/v1/tracker/lists":                                   {http.StatusConflict, apiv1.ErrorCodeTrackerUnsupported},
		"/api/v1/tracker/lists/42/items?media=movies":             {http.StatusConflict, apiv1.ErrorCodeTrackerUnsupported},
		"/api/v1/tracker/lists/status:watching/items?media=shows": {http.StatusConflict, apiv1.ErrorCodeTrackerUnsupported}, // a valid Simkl id
		"/api/v1/tracker/shows/watchlist":                         {http.StatusBadGateway, apiv1.ErrorCodeInternalError},
	} {
		w := do(newEngine(), request{path: path})
		if w.Code != want.status || errorCode(t, w) != want.code {
			t.Errorf("%s: status %d: %s", path, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "secret") {
			t.Errorf("%s: upstream error leaked: %s", path, w.Body)
		}
	}
}

func TestTrackerNotLinked(t *testing.T) {
	setCreds(t, "", "")
	useTracker(t, fakeTracker{authorized: false})
	for _, path := range []string{
		"/api/v1/tracker/movies/watchlist",
		"/api/v1/tracker/shows/collection",
		"/api/v1/tracker/shows/progress",
		"/api/v1/tracker/lists",
		"/api/v1/tracker/lists/42/items?media=shows",
	} {
		w := do(newEngine(), request{path: path})
		if w.Code != http.StatusConflict || errorCode(t, w) != apiv1.ErrorCodeTrackerNotAuthorized {
			t.Errorf("%s: status %d: %s", path, w.Code, w.Body)
		}
	}
}

func TestLibrary(t *testing.T) {
	setCreds(t, "", "")
	stubTMDB(t)
	for _, li := range []database.LibraryItem{
		{ID: 11, MediaType: library.MovieType, State: library.StateActive},
		{ID: 12, MediaType: library.MovieType, State: library.StateDeleted},
		{ID: 13, MediaType: library.MovieType, State: library.StateActive},
		{ID: 1396, ShowID: 1396, MediaType: library.ShowType, State: library.StateActive},
	} {
		li.Key = database.LibraryItemKey(li.MediaType, li.ID)
		if err := database.GetStormDB().Save(&li); err != nil {
			t.Fatal(err)
		}
	}

	p := getPage(t, "/api/v1/library/movies?page=2&limit=1")
	if got := titles(p.Items); !reflect.DeepEqual(got, []string{"13:m13"}) || p.TotalPages != 2 || *p.TotalResults != 2 {
		t.Errorf("movies = %v, %+v", got, p)
	}
	if got := titles(getPage(t, "/api/v1/library/shows").Items); !reflect.DeepEqual(got, []string{"1396:s1396"}) {
		t.Errorf("shows = %v", got)
	}
}
