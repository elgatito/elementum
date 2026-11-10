package jsonapi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/api"
	"github.com/elgatito/elementum/api/json"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"
)

type watchedTracker struct {
	fakeTracker
	write func([]*tracker.WatchedItem, bool) error
}

func (p watchedTracker) SetWatched(items []*tracker.WatchedItem, watched bool) error {
	return p.write(items, watched)
}

func isolateWatched(t *testing.T) {
	t.Helper()
	playcount.Mu.Lock()
	old := playcount.Watched
	playcount.Watched = map[uint64]playcount.WatchedState{}
	playcount.Mu.Unlock()
	oldShows := uid.Get().Shows
	uid.Get().Shows = nil
	oldHost, oldHosts := xbmc.XBMCLocalHost, xbmc.XBMCHosts
	xbmc.XBMCLocalHost, xbmc.XBMCHosts = nil, nil
	oldSync := config.Get().TraktSyncEnabled
	config.Get().TraktSyncEnabled = false
	t.Cleanup(func() {
		playcount.Mu.Lock()
		playcount.Watched = old
		playcount.Mu.Unlock()
		uid.Get().Shows = oldShows
		xbmc.XBMCLocalHost, xbmc.XBMCHosts = oldHost, oldHosts
		config.Get().TraktSyncEnabled = oldSync
	})
}

const watchedShow = 990031

func watchedMetadata(t *testing.T) {
	t.Helper()
	stubAPI(t, reqapi.TMDBAPI, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tv/990031":
			fmt.Fprint(w, `{"id":990031,"name":"Show","seasons":[{"season_number":0,"episode_count":1},{"season_number":1,"episode_count":2},{"season_number":2,"episode_count":3},{"season_number":3,"episode_count":1}]}`)
		case "/tv/990031/season/0":
			fmt.Fprint(w, `{"id":100,"season_number":0,"episodes":[{"id":1,"name":"Special","overview":"x","season_number":0,"episode_number":1,"air_date":"2000-01-01"}]}`)
		case "/tv/990031/season/1":
			fmt.Fprint(w, `{"id":101,"season_number":1,"episodes":[{"id":11,"name":"One","overview":"x","season_number":1,"episode_number":1,"air_date":"2000-01-01"},{"id":12,"name":"Two","overview":"x","season_number":1,"episode_number":2,"air_date":"2000-01-02"}]}`)
		case "/tv/990031/season/2":
			fmt.Fprint(w, `{"id":102,"season_number":2,"episodes":[{"id":21,"name":"One","overview":"x","season_number":2,"episode_number":1,"air_date":"2000-01-01"},{"id":22,"name":"Future","overview":"x","season_number":2,"episode_number":2,"air_date":"2999-01-01"},{"id":23,"name":"Undated","overview":"x","season_number":2,"episode_number":3,"air_date":""}]}`)
		case "/tv/990031/season/3":
			fmt.Fprint(w, `{"id":103,"season_number":3,"episodes":[{"id":31,"name":"Future","overview":"x","season_number":3,"episode_number":1,"air_date":"2999-01-01"}]}`)
		case "/movie/990031":
			fmt.Fprint(w, `{"id":990031,"title":"Movie"}`)
		default:
			http.NotFound(w, r)
		}
	})
}

func TestWatchedActionToRead(t *testing.T) {
	setCreds(t, "", "")
	isolateWatched(t)
	watchedMetadata(t)
	var last tracker.Item
	useTracker(t, watchedTracker{fakeTracker: fakeTracker{authorized: true}, write: func(items []*tracker.WatchedItem, _ bool) error {
		if len(items) != 1 {
			t.Fatalf("tracker items = %v", items)
		}
		last = items[0].Item
		return nil
	}})
	e := newEngine()
	action := func(method, kind, extra string) {
		t.Helper()
		w := do(e, request{method: method, path: "/api/watched/" + kind, body: `{"tmdb_id":990031` + extra + `}`})
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s %s: %d %s", method, kind, w.Code, w.Body)
		}
	}
	readShow := func() jsonapi.ShowDetails {
		t.Helper()
		w := do(e, request{path: "/api/shows/990031"})
		var d jsonapi.ShowDetails
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != http.StatusOK {
			t.Fatalf("show read: %d %s (%v)", w.Code, w.Body, err)
		}
		return d
	}
	readEpisodes := func(season int, want ...bool) {
		t.Helper()
		w := do(e, request{path: fmt.Sprintf("/api/shows/990031/seasons/%d", season)})
		var d jsonapi.Season
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != http.StatusOK {
			t.Fatalf("season read: %d %s (%v)", w.Code, w.Body, err)
		}
		if len(d.Episodes) != len(want) {
			t.Fatalf("episodes = %v", d.Episodes)
		}
		for i, ep := range d.Episodes {
			if ep.Watched == nil || *ep.Watched != want[i] {
				t.Errorf("S%d E%d watched = %v, want %v", season, i+1, ep.Watched, want[i])
			}
		}
	}
	checkSeason := func(number, count int, watched bool) {
		t.Helper()
		s := readShow().Seasons[number]
		if s.Watched == nil || *s.Watched != watched || s.WatchedEpisodes == nil || *s.WatchedEpisodes != count {
			t.Fatalf("season %d: %+v", number, s)
		}
	}

	action("POST", "season", `,"season":1`)
	if last.Type != tracker.SeasonType || last.IDs.TMDB != watchedShow || last.Season != 1 {
		t.Fatalf("tracker item = %+v", last)
	}
	readEpisodes(1, true, true)
	checkSeason(1, 2, true)
	action("DELETE", "season", `,"season":1`)
	readEpisodes(1, false, false)
	checkSeason(1, 0, false)
	action("POST", "show", "")
	readEpisodes(0, true)
	readEpisodes(1, true, true)
	readEpisodes(2, true, false, false)
	readEpisodes(3, false)
	checkSeason(2, 1, false)
	checkSeason(3, 0, false)
	if d := readShow(); d.Watched == nil || *d.Watched {
		t.Fatalf("running show must stay incomplete: %+v", d.Watched)
	}
	// Existing tracker/Kodi parent marks must not mask a descendant unwatch.
	playcount.Set(playcount.ShowType, true, watchedShow)
	playcount.Set(playcount.SeasonType, true, watchedShow, 1)
	action("DELETE", "episode", `,"season":1,"episode":1`)
	readEpisodes(1, false, true)
	checkSeason(1, 1, false)
	if d := readShow(); d.Watched == nil || *d.Watched || bool(playcount.GetWatchedShowByTMDB(watchedShow)) || bool(playcount.GetWatchedSeasonByTMDB(watchedShow, 1)) {
		t.Fatal("unwatching an episode retained a parent mark")
	}
	action("DELETE", "show", "")
	readEpisodes(0, false)
	readEpisodes(1, false, false)
	readEpisodes(2, false, false, false)
	checkSeason(1, 0, false)
	for _, method := range []string{"POST", "DELETE"} {
		action(method, "movie", "")
		w := do(e, request{path: "/api/movies/990031"})
		var d jsonapi.MovieDetails
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil || w.Code != http.StatusOK || d.Watched == nil || *d.Watched != (method == "POST") {
			t.Fatalf("movie after %s: %d %s", method, w.Code, w.Body)
		}
	}
}

func TestWatchedTrackerFailureLeavesLocalState(t *testing.T) {
	setCreds(t, "", "")
	isolateWatched(t)
	watchedMetadata(t)
	useTracker(t, watchedTracker{fakeTracker: fakeTracker{authorized: true}, write: func([]*tracker.WatchedItem, bool) error {
		return errors.New("offline https://private.example/?key=secret")
	}})
	playcount.Set(playcount.ShowType, true, watchedShow)
	playcount.Set(playcount.SeasonType, true, watchedShow, 1)
	playcount.Set(playcount.EpisodeType, true, watchedShow, 1, 1)
	playcount.Mu.RLock()
	before := maps.Clone(playcount.Watched)
	playcount.Mu.RUnlock()
	for _, kind := range []string{"movie", "show", "season", "episode"} {
		for _, method := range []string{"POST", "DELETE"} {
			w := do(newEngine(), request{method: method, path: "/api/watched/" + kind, body: `{"tmdb_id":990031,"season":1,"episode":1}`})
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "secret") {
				t.Fatalf("%s %s: %d %s", method, kind, w.Code, w.Body)
			}
			playcount.Mu.RLock()
			unchanged := reflect.DeepEqual(before, playcount.Watched)
			playcount.Mu.RUnlock()
			if !unchanged {
				t.Fatalf("%s %s changed local state", method, kind)
			}
		}
	}
}

// kodiWatchedRPC serves both Kodi's library RPC and the add-on fallback on one test port.
func kodiWatchedRPC(t *testing.T) (<-chan string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	oldPort, oldExPort := xbmc.XBMCJSONRPCPort, xbmc.XBMCExJSONRPCPort
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	xbmc.XBMCJSONRPCPort, xbmc.XBMCExJSONRPCPort = port, port
	t.Cleanup(func() {
		listener.Close()
		xbmc.XBMCJSONRPCPort, xbmc.XBMCExJSONRPCPort = oldPort, oldExPort
	})
	methods := make(chan string, 10)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				var req struct {
					ID     uint64 `json:"id"`
					Method string `json:"method"`
				}
				if json.NewDecoder(conn).Decode(&req) == nil {
					methods <- req.Method
					json.NewEncoder(conn).Encode(map[string]any{"id": req.ID, "result": "OK"})
				}
			}()
		}
	}()
	return methods, "127.0.0.1"
}

func TestLegacyWatchedKodiSetterAndFallback(t *testing.T) {
	for _, tc := range []struct {
		kind, method string
		present      bool
	}{
		{"season", "ToggleWatched", false},
		{"episode", "ToggleWatched", false},
		{"season", "VideoLibrary.SetSeasonDetails", true},
		{"episode", "VideoLibrary.SetEpisodeDetails", true},
	} {
		t.Run(tc.method, func(t *testing.T) {
			isolateWatched(t)
			watchedMetadata(t)
			show := &uid.Show{ID: 10, UIDs: &uid.UniqueIDs{TMDB: watchedShow}}
			if tc.present {
				show.Seasons = []*uid.Season{{ID: 11, Season: 1}}
				show.Episodes = []*uid.Episode{{ID: 12, Season: 1, Episode: 1}}
			}
			uid.Get().Shows = []*uid.Show{show}
			methods, selected := kodiWatchedRPC(t)
			// A wrong local host makes this test fail if the request-selected host is lost.
			xbmc.XBMCLocalHost = &xbmc.XBMCHost{Host: "192.0.2.1"}
			xbmc.XBMCHosts = []*xbmc.XBMCHost{{Host: selected}}
			entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			useTracker(t, watchedTracker{fakeTracker: fakeTracker{authorized: true}, write: func([]*tracker.WatchedItem, bool) error {
				close(entered)
				<-release
				defer close(finished)
				return errors.New("offline https://private.example/?key=secret")
			}})
			r := gin.New()
			r.GET("/show/:showId/season/:season/episode/:episode", api.ToggleWatched(tc.kind, true))
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- do(r, request{path: "/show/990031/season/1/episode/1?kodi=" + selected}) }()
			select {
			case w := <-done:
				if w.Code != http.StatusOK {
					t.Errorf("status %d", w.Code)
				}
			case <-time.After(3 * time.Second):
				t.Error("Kodi action waited for the tracker")
			}
			select {
			case method := <-methods:
				if method != tc.method {
					t.Errorf("Kodi method = %s", method)
				}
			case <-time.After(time.Second):
				t.Errorf("Kodi did not receive %s", tc.method)
			}
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Error("tracker update did not start")
			}
			close(release)
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("tracker update did not finish")
			}
			playcount.Mu.RLock()
			changed := len(playcount.Watched) != 0
			playcount.Mu.RUnlock()
			if changed {
				t.Error("tracker failure changed local playcounts")
			}
		})
	}
}
