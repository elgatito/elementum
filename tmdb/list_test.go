package tmdb

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"
)

// TestMain opens a real cache DB before any request. Without one every cached request's write fails, and
// reqapi.Request.Do's async cache write can then race that error into Do's return value (request.go:164).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tmdb-test")
	if err != nil {
		panic(err)
	}
	if _, err := database.InitCacheDB(&config.Configuration{Info: &xbmc.AddonInfo{Profile: dir}}); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// stubTMDB points reqapi at a fresh server for this (sub)test, so cache keys (which include the URL)
// never collide between subtests. It serves 55 results as TMDB pages of 20 (ids 101-120, 201-220,
// 301-315) and /movie/<id>; page 2 of /broken fails. It returns the highest page asked for.
func stubTMDB(t *testing.T) *atomic.Int64 {
	var maxPage atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id, ok := strings.CutPrefix(r.URL.Path, "/movie/"); ok {
			fmt.Fprintf(w, `{"id": %s, "title": "m%s"}`, id, id)
			return
		}
		p, _ := strconv.Atoi(r.URL.Query().Get("page"))
		for old := maxPage.Load(); int64(p) > old && !maxPage.CompareAndSwap(old, int64(p)); old = maxPage.Load() {
		}
		if p > 3 || p == 2 && r.URL.Path == "/broken" {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		ids := []string{}
		for i := 1; i <= min(20, 55-(p-1)*20); i++ {
			ids = append(ids, fmt.Sprintf(`{"id": %d}`, p*100+i))
		}
		fmt.Fprintf(w, `{"page": %d, "total_results": 55, "results": [%s]}`, p, strings.Join(ids, ","))
	}))
	old := reqapi.TMDBAPI.Endpoint
	reqapi.TMDBAPI.Endpoint = srv.URL
	t.Cleanup(func() { reqapi.TMDBAPI.Endpoint = old; srv.Close() })
	return &maxPage
}

func ids(movies Movies) (out []int) {
	for _, m := range movies {
		id := 0
		if m != nil {
			id = m.ID
		}
		out = append(out, id)
	}
	return
}

func TestListMoviesPerPage(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint string
		page, perPage  int
		want           map[int]int // slot -> id, 0 for an empty slot
		total          int
	}{
		{"spans two TMDB pages", "list", 1, 30, map[int]int{0: 101, 19: 120, 20: 201, 29: 210}, 55},
		{"last page is short", "list", 2, 30, map[int]int{0: 211, 9: 220, 10: 301, 24: 315, 25: 0, 29: 0}, 55},
		{"failed TMDB page leaves holes", "broken", 1, 30, map[int]int{0: 101, 19: 120, 20: 0, 29: 0}, 55},
		{"only TMDB page fails", "broken", 2, 20, map[int]int{0: 0, 19: 0}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubTMDB(t)
			movies, total := listMovies(tc.endpoint, napping.Params{"language": "en"}, tc.page, tc.perPage)
			got := ids(movies)
			if len(got) != tc.perPage || total != tc.total {
				t.Fatalf("ids %v, total %d", got, total)
			}
			for slot, id := range tc.want {
				if got[slot] != id {
					t.Errorf("slot %d = %d, want %d (ids %v)", slot, got[slot], id, got)
				}
			}
		})
	}

	t.Run("never asks for TMDB page 501", func(t *testing.T) {
		maxPage := stubTMDB(t)
		// The slots of page 334 at 30 per page end in TMDB page 501, which TMDB rejects.
		listMovies("list", napping.Params{"language": "en"}, 334, 30)
		if maxPage.Load() != TMDBMaxPages {
			t.Errorf("highest TMDB page asked for: %d, want %d", maxPage.Load(), TMDBMaxPages)
		}
	})
}

func liteStub(t *testing.T, details, lists *atomic.Int64, kind string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/movie/"), strings.HasPrefix(r.URL.Path, "/tv/"):
			details.Add(1)
			fmt.Fprint(w, `{"id": 2}`)
		case strings.HasPrefix(r.URL.Path, "/genre/"):
			fmt.Fprint(w, `{"genres": [{"id": 7, "name": "Drama"}]}`)
		case r.URL.Query().Get("language") == "en-US": // the fallback page
			lists.Add(1)
			fmt.Fprintf(w, `{"total_results": 2, "results": [{"id": 1, "overview": "en one"}, {"id": 2, "overview": "en two"}]}`)
		default: // item 2 has no overview in this language
			fmt.Fprintf(w, `{"total_results": 2, "results": [{"id": 1, "%s": "one", "overview": "o", "genre_ids": [7, 99]}, {"id": 2, "%s": "two"}]}`, kind, kind)
		}
	}))
	t.Cleanup(srv.Close)
	old := reqapi.TMDBAPI.Endpoint
	reqapi.TMDBAPI.Endpoint = srv.URL
	t.Cleanup(func() { reqapi.TMDBAPI.Endpoint = old })
}

func TestLiteSkipsDetails(t *testing.T) {
	var details, lists atomic.Int64
	liteStub(t, &details, &lists, "title")
	movies, _ := listMovies("lite", napping.Params{"language": "uk"}, 1, 2, Lite)
	if movies[0].Title != "one" || len(movies[0].Genres) != 1 || movies[0].Genres[0].Name != "Drama" || movies[0].Overview != "o" {
		t.Errorf("lite item: %+v", movies[0])
	}
	if movies[1].Title != "two" || movies[1].Overview != "en two" || details.Load() != 0 || lists.Load() != 1 {
		t.Errorf("fallback item %+v, detail fetches %d, en-US pages %d", movies[1], details.Load(), lists.Load())
	}
}

func TestLiteShows(t *testing.T) {
	var details, lists atomic.Int64
	liteStub(t, &details, &lists, "name")
	shows, _ := listShows("lite-tv", napping.Params{"language": "uk"}, 1, 2, Lite)
	if shows[0].Name != "one" || len(shows[0].Genres) != 1 || shows[1].Overview != "en two" || details.Load() != 0 {
		t.Errorf("shows %+v %+v, detail fetches %d", shows[0], shows[1], details.Load())
	}
}
