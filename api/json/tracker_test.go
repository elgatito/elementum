package jsonapi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elgatito/elementum/api/json"
	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"
)

// TestMain opens real cache and library DBs: cached TMDB/Trakt requests, tracker activities and the library need them.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jsonapi-test")
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

func getPage(t *testing.T, path string) jsonapi.MediaItemPage {
	t.Helper()
	w := do(newEngine(), request{path: path})
	var page jsonapi.MediaItemPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || w.Code != http.StatusOK {
		t.Fatalf("%s: status %d, %v: %s", path, w.Code, err, w.Body)
	}
	return page
}

func titles(items []jsonapi.MediaItem) (out []string) {
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

	p := getPage(t, "/api/trakt/movies/lists/trending?page=2&limit=7")
	if len(p.Items) != 7 || p.Items[0].TmdbId != 200 || p.Items[0].Title != "m200" || p.Items[0].Type != jsonapi.MediaTypeMovie {
		t.Errorf("items = %v", titles(p.Items))
	}
	if p.Page != 2 || p.Limit != 7 || p.TotalPages != 5 || p.TotalResults == nil || *p.TotalResults != 30 {
		t.Errorf("page = %+v", p)
	}
	// Another limit must not be served from the first one's cache entry; the same one must.
	getPage(t, "/api/trakt/movies/lists/trending?page=2&limit=5")
	getPage(t, "/api/trakt/movies/lists/trending?page=2&limit=7")
	if got, want := seen(), []string{"/movies/trending?page=2&limit=7", "/movies/trending?page=2&limit=5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Trakt requests = %v, want %v", got, want)
	}

	if w := do(newEngine(), request{path: "/api/trakt/shows/lists/popular"}); w.Code != http.StatusBadGateway {
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

	p := getPage(t, "/api/tracker/movies/watchlist?page=1&limit=2")
	if got := titles(p.Items); !reflect.DeepEqual(got, []string{"11:m11", "12:m12"}) { // 12 resolved by IMDB id
		t.Errorf("page 1 = %v", got)
	}
	if p.TotalPages != 3 || p.TotalResults == nil || *p.TotalResults != 5 {
		t.Errorf("page 1 = %+v", p)
	}
	if got := titles(getPage(t, "/api/tracker/movies/watchlist?page=3&limit=2").Items); !reflect.DeepEqual(got, []string{"15:m15"}) {
		t.Errorf("page 3 = %v", got)
	}
	if p := getPage(t, "/api/tracker/movies/watchlist?page=9223372036854775807&limit=2"); p.Items == nil || len(p.Items) != 0 {
		t.Errorf("past the end = %+v", p)
	}

	w := do(newEngine(), request{path: "/api/tracker/shows/progress?lang=de-DE"})
	var np jsonapi.NextEpisodePage
	if err := json.Unmarshal(w.Body.Bytes(), &np); err != nil || w.Code != http.StatusOK || len(np.Items) != 1 {
		t.Fatalf("progress: status %d, %v: %s", w.Code, err, w.Body)
	}
	if e := np.Items[0]; e.Show.TmdbId != 1396 || e.Show.Title != "s1396" || e.Season != 2 || e.Episode != 5 ||
		e.Title == nil || *e.Title != "e5" || e.AirDate == nil || e.Still == nil {
		t.Errorf("progress item = %+v", e)
	}

	for path, want := range map[string]struct {
		status int
		code   jsonapi.ErrorCode
	}{
		"/api/tracker/movies/collection":                       {http.StatusConflict, jsonapi.ErrorCodeTrackerUnsupported},
		"/api/tracker/lists":                                   {http.StatusConflict, jsonapi.ErrorCodeTrackerUnsupported},
		"/api/tracker/lists/42/items?media=movies":             {http.StatusConflict, jsonapi.ErrorCodeTrackerUnsupported},
		"/api/tracker/lists/status:watching/items?media=shows": {http.StatusConflict, jsonapi.ErrorCodeTrackerUnsupported}, // a valid Simkl id
		"/api/tracker/shows/watchlist":                         {http.StatusBadGateway, jsonapi.ErrorCodeInternalError},
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

// expiredTracker fails every list call as a revoked token does.
type expiredTracker struct{ fakeTracker }

func (expiredTracker) Lists() ([]*tracker.List, error) {
	return nil, fmt.Errorf("upstream: %w", util.ErrUnauthorized)
}

// ListItems completes tracker.ListReader.
func (expiredTracker) ListItems(string, tracker.MediaType, bool) ([]*tracker.Item, error) {
	return nil, util.ErrUnauthorized
}

func TestTrackerTokenRevoked(t *testing.T) {
	setCreds(t, "", "")
	useTracker(t, expiredTracker{fakeTracker{authorized: true}})
	w := do(newEngine(), request{path: "/api/tracker/lists"})
	if w.Code != http.StatusConflict || errorCode(t, w) != jsonapi.ErrorCodeTrackerNotAuthorized {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
}

func TestTrackerNotLinked(t *testing.T) {
	setCreds(t, "", "")
	useTracker(t, fakeTracker{authorized: false})
	for _, path := range []string{
		"/api/tracker/movies/watchlist",
		"/api/tracker/shows/collection",
		"/api/tracker/shows/progress",
		"/api/tracker/lists",
		"/api/tracker/lists/42/items?media=shows",
	} {
		w := do(newEngine(), request{path: path})
		if w.Code != http.StatusConflict || errorCode(t, w) != jsonapi.ErrorCodeTrackerNotAuthorized {
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

	p := getPage(t, "/api/library/movies?page=2&limit=1")
	if got := titles(p.Items); !reflect.DeepEqual(got, []string{"13:m13"}) || p.TotalPages != 2 || *p.TotalResults != 2 {
		t.Errorf("movies = %v, %+v", got, p)
	}
	if got := titles(getPage(t, "/api/library/shows").Items); !reflect.DeepEqual(got, []string{"1396:s1396"}) {
		t.Errorf("shows = %v", got)
	}
}

func TestItemState(t *testing.T) {
	setCreds(t, "", "")
	stubTMDB(t)
	li := database.LibraryItem{ID: 21, MediaType: library.MovieType, State: library.StateActive}
	li.Key = database.LibraryItemKey(li.MediaType, li.ID)
	if err := database.GetStormDB().Save(&li); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.GetStormDB().DeleteStruct(&li) })
	playcount.Set(playcount.MovieType, true, 22)
	t.Cleanup(func() { playcount.Set(playcount.MovieType, false, 22) })

	got := map[int]jsonapi.MediaItem{}
	for _, i := range getPage(t, "/api/library/movies").Items {
		got[i.TmdbId] = i
	}
	if i := got[21]; i.InLibrary == nil || !*i.InLibrary || i.InCollection != nil || i.InWatchlist != nil || i.Watched != nil {
		t.Errorf("library movie without a tracker: %+v", i)
	}
	// An item in the Kodi library's watched state is watched, whatever the tracker.
	w := do(newEngine(), request{path: "/api/movies/22"})
	var d jsonapi.MovieDetails
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || d.InLibrary == nil || *d.InLibrary || d.Watched == nil || !*d.Watched {
		t.Errorf("details %d: %+v, %v", w.Code, d, err)
	}
}

func TestActions(t *testing.T) {
	setCreds(t, "", "")
	stubTMDB(t)
	e := newEngine()
	before := fmt.Sprint(playcountSnapshot())
	for name, tc := range map[string]struct {
		method, path, body string
		want               int
	}{
		"bad kind":              {"POST", "/api/watched/series", `{"tmdb_id":1}`, 400},
		"no body fields":        {"POST", "/api/watched/movie", `{}`, 400},
		"season needs season":   {"POST", "/api/watched/season", `{"tmdb_id":1}`, 400},
		"episode needs episode": {"DELETE", "/api/watched/episode", `{"tmdb_id":1,"season":1}`, 400},
		"no tracker, no Kodi":   {"POST", "/api/watched/movie", `{"tmdb_id":603}`, 409},
		"unwatch, same":         {"DELETE", "/api/watched/show", `{"tmdb_id":1396}`, 409},
		"bad media":             {"POST", "/api/library/songs/1", ``, 400},
		"bad id":                {"DELETE", "/api/library/movies/0", ``, 400},
		"remove when absent":    {"DELETE", "/api/library/movies/9999", ``, 204},
	} {
		t.Run(name, func(t *testing.T) {
			if w := do(e, request{method: tc.method, path: tc.path, body: tc.body}); w.Code != tc.want {
				t.Errorf("%d: %s", w.Code, w.Body)
			}
		})
	}
	if after := fmt.Sprint(playcountSnapshot()); after != before {
		t.Errorf("a refused action changed the watched map:\n%s\n%s", before, after)
	}
}

func playcountSnapshot() []uint64 {
	playcount.Mu.RLock()
	defer playcount.Mu.RUnlock()
	keys := []uint64{}
	for k := range playcount.Watched {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestStoredResume(t *testing.T) {
	setCreds(t, "", "")
	stubTMDB(t)
	database.GetCache().SetCachedObject(database.CommonBucket, 3600, bittorrent.TMDBResumeKey(false, 31, 0, 0), map[string]float64{"position": 300, "total": 1200})
	t.Cleanup(func() { database.GetCache().Delete(database.CommonBucket, bittorrent.TMDBResumeKey(false, 31, 0, 0)) })
	w := do(newEngine(), request{path: "/api/movies/31"})
	var d jsonapi.MovieDetails
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || d.Progress == nil || *d.Progress != 0.25 {
		t.Errorf("progress %d: %+v, %v", w.Code, d.Progress, err)
	}
}

func TestLibraryAddRemoveAdd(t *testing.T) {
	setCreds(t, "", "")
	stubTMDB(t)
	conf := config.Get()
	oldPath, oldInfo := conf.LibraryPath, conf.Info
	conf.LibraryPath, conf.Info = t.TempDir(), &xbmc.AddonInfo{ID: "plugin.video.elementum", Profile: t.TempDir()}
	t.Cleanup(func() {
		conf.LibraryPath, conf.Info = oldPath, oldInfo
		library.RemoveMovie(41, false)
		database.GetStormDB().Drop(&database.LibraryItem{})
	})
	e := newEngine()
	for i, step := range []struct{ method string }{{"POST"}, {"POST"}, {"DELETE"}, {"POST"}} {
		if w := do(e, request{method: step.method, path: "/api/library/movies/41"}); w.Code != 204 {
			t.Fatalf("step %d %s: %d %s", i, step.method, w.Code, w.Body)
		}
		if want := step.method == "POST"; library.IsInLibrary(41, library.MovieType) != want {
			t.Fatalf("step %d %s: in library = %v", i, step.method, !want)
		}
	}
}

type authTracker struct {
	fakeTracker
	err error
}

func (a authTracker) StartAuth() (*tracker.DeviceCode, error) {
	if a.err != nil {
		return nil, a.err
	}
	return &tracker.DeviceCode{UserCode: "ABCD", VerificationURL: "https://x/activate", ExpiresIn: 600, Interval: 5}, nil
}

func TestStartTrackerAuth(t *testing.T) {
	setCreds(t, "", "")
	post := request{method: http.MethodPost, path: "/api/tracker/authorize"}

	useTracker(t, authTracker{})
	w := do(newEngine(), post)
	var got jsonapi.TrackerAuthCode
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK ||
		got.UserCode != "ABCD" || got.VerificationUrl != "https://x/activate" || got.ExpiresIn != 600 || got.Interval != 5 || got.Tracker.Id != "fake" {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}

	useTracker(t, fakeTracker{}) // no StartAuth
	w = do(newEngine(), post)
	if w.Code != http.StatusConflict || errorCode(t, w) != jsonapi.ErrorCodeTrackerUnsupported {
		t.Errorf("unsupported: status %d: %s", w.Code, w.Body)
	}

	useTracker(t, authTracker{err: errors.New("down")})
	if w = do(newEngine(), post); w.Code != http.StatusBadGateway {
		t.Errorf("failing: status %d: %s", w.Code, w.Body)
	}

	config.Get().TrackerProvider = "" // none selected
	w = do(newEngine(), post)
	if w.Code != http.StatusConflict || errorCode(t, w) != jsonapi.ErrorCodeTrackerUnsupported {
		t.Errorf("none: status %d: %s", w.Code, w.Body)
	}
}

func TestStartTrackerAuthYAMLSettings(t *testing.T) {
	setCreds(t, "", "")
	useTracker(t, authTracker{})
	old := config.Args.ConfigPath
	t.Cleanup(func() { config.Args.ConfigPath = old })
	config.Args.ConfigPath = "settings.yaml"
	w := do(newEngine(), request{method: http.MethodPost, path: "/api/tracker/authorize"})
	if w.Code != http.StatusConflict || errorCode(t, w) != jsonapi.ErrorCodeTrackerUnsupported {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
}

// noLists is a linked tracker whose account has no lists.
type noLists struct{ fakeTracker }

func (noLists) Lists() ([]*tracker.List, error) { return nil, nil }
func (noLists) ListItems(string, tracker.MediaType, bool) ([]*tracker.Item, error) {
	return nil, nil
}

// An account without lists answers an empty array, not null.
func TestTrackerListsEmpty(t *testing.T) {
	setCreds(t, "", "")
	useTracker(t, noLists{fakeTracker{authorized: true}})
	w := do(newEngine(), request{path: "/api/tracker/lists"})
	if w.Code != http.StatusOK || w.Body.String() != "[]" {
		t.Errorf("status %d: %s", w.Code, w.Body)
	}
}
