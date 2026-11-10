package jsonapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/elgatito/elementum/api/json"
	"github.com/elgatito/elementum/util/reqapi"
)

func TestDetailBoundsRejectBeforeUpstream(t *testing.T) {
	setCreds(t, "", "")
	seen := stubAPI(t, reqapi.TMDBAPI, http.NotFound)
	r := newEngine()
	paths := []string{
		"/api/movies/%d", "/api/shows/%d", "/api/people/%d",
		"/api/people/%d/credits", "/api/shows/%d/seasons/0",
		"/api/movies/%d/streams", "/api/shows/%d/seasons/0/episodes/1/streams",
	}
	var invalid []string
	for _, path := range paths {
		for _, id := range []int{-1, 0} {
			invalid = append(invalid, fmt.Sprintf(path, id))
		}
	}
	invalid = append(invalid,
		"/api/shows/1/seasons/-1",
		"/api/shows/1/seasons/-1/episodes/1/streams",
		"/api/shows/1/seasons/0/episodes/0/streams",
		"/api/shows/1/seasons/0/episodes/-1/streams",
	)
	for _, path := range invalid {
		t.Run(path, func(t *testing.T) {
			w := do(r, request{path: path})
			if w.Code != http.StatusBadRequest || errorCode(t, w) != jsonapi.ErrorCodeBadRequest {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if calls := seen(); len(calls) != 0 {
				t.Fatalf("invalid request called TMDB: %v", calls)
			}
		})
	}
}

func TestInvalidLanguageRejectsBeforeUpstream(t *testing.T) {
	setCreds(t, "", "")
	tmdbCalls := stubAPI(t, reqapi.TMDBAPI, http.NotFound)
	traktCalls := stubAPI(t, reqapi.TraktAPI, http.NotFound)
	r := newEngine()
	for _, path := range []string{
		"/api/movies/1?", "/api/shows/1?", "/api/shows/1/seasons/0?",
		"/api/people/1?", "/api/people/1/credits?",
		"/api/movies/genres?", "/api/shows/genres?",
		"/api/movies/lists/popular?", "/api/shows/lists/popular?",
		"/api/search?query=matrix&type=movie&",
		"/api/trakt/movies/lists/trending?", "/api/library/movies?",
		"/api/tracker/movies/watchlist?", "/api/ui/events?client_id=lang-test&wait=0&",
	} {
		t.Run(path, func(t *testing.T) {
			for _, lang := range []string{"", "deutsch", "de-DE-extra", "../en"} {
				w := do(r, request{path: path + "lang=" + url.QueryEscape(lang)})
				if w.Code != http.StatusBadRequest || errorCode(t, w) != jsonapi.ErrorCodeBadRequest {
					t.Errorf("lang %q: status %d: %s", lang, w.Code, w.Body)
				}
			}
		})
	}
	if tmdb, trakt := tmdbCalls(), traktCalls(); len(tmdb)+len(trakt) != 0 {
		t.Fatalf("invalid language called upstream: TMDB %v, Trakt %v", tmdb, trakt)
	}
}

func TestSeasonZeroAndLanguage(t *testing.T) {
	setCreds(t, "", "")
	seen := stubAPI(t, reqapi.TMDBAPI, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/987601/season/0" || r.URL.Query().Get("language") != "pt-BR" {
			t.Errorf("unexpected TMDB request: %s", r.URL.Path+"?language="+r.URL.Query().Get("language"))
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"id":987602,"season_number":0,"name":"Specials","episodes":[]}`)
	})
	w := do(newEngine(), request{path: "/api/shows/987601/seasons/0?lang=PT_br"})
	var season jsonapi.Season
	if err := json.Unmarshal(w.Body.Bytes(), &season); err != nil || w.Code != http.StatusOK {
		t.Fatalf("season zero: status %d, %v: %s", w.Code, err, w.Body)
	}
	if season.SeasonNumber != 0 || season.Name != "Specials" || season.Episodes == nil || len(seen()) != 1 {
		t.Errorf("season = %+v, requests = %v", season, seen())
	}
}
