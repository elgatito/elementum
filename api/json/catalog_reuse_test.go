package jsonapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/elgatito/elementum/api/json"
	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/util/reqapi"
)

func TestBulkMediaResolution(t *testing.T) {
	setCreds(t, "", "")
	for _, media := range []struct{ plural, singular, endpoint, title, resolved string }{
		{"movies", "movie", "movie", "title", "92"},
		{"shows", "show", "tv", "name", "95"},
	} {
		t.Run(media.plural, func(t *testing.T) {
			seen := stubAPI(t, reqapi.TMDBAPI, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/find/ttfallback":
					fmt.Fprint(w, `{"movie_results":[{"id":92}],"tv_results":[{"id":95}]}`)
				case "/find/ttunknown":
					fmt.Fprint(w, `{"movie_results":[],"tv_results":[]}`)
				case "/" + media.endpoint + "/91", "/" + media.endpoint + "/" + media.resolved:
					if r.URL.Query().Get("language") != "de-DE" {
						t.Errorf("language = %q, want de-DE", r.URL.Query().Get("language"))
					}
					id := r.URL.Path[len(r.URL.Path)-2:]
					fmt.Fprintf(w, `{"id":%s,%q:"item%s"}`, id, media.title, id)
				case "/" + media.endpoint + "/94":
					fmt.Fprint(w, `{}`) // A non-nil response with no ID is also omitted.
				default:
					http.NotFound(w, r)
				}
			})
			stubAPI(t, reqapi.TraktAPI, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/"+media.plural+"/trending" {
					http.NotFound(w, r)
					return
				}
				ids := []string{
					`{"tmdb":91,"imdb":"ttmustnotresolve"}`, `{"imdb":"ttfallback"}`,
					`{}`, `{"tmdb":93}`, `{"tmdb":94}`, `{"imdb":"ttunknown"}`, `{"tmdb":91}`,
				}
				w.Header().Set("X-Pagination-Item-Count", "7")
				fmt.Fprint(w, "[")
				for i, id := range ids {
					if i != 0 {
						fmt.Fprint(w, ",")
					}
					fmt.Fprintf(w, `{%q:{"ids":%s}}`, media.singular, id)
				}
				fmt.Fprint(w, "]")
			})
			page := getPage(t, "/api/trakt/"+media.plural+"/lists/trending?lang=DE_de")
			if got, want := titles(page.Items), []string{"91:item91", media.resolved + ":item" + media.resolved, "91:item91"}; !reflect.DeepEqual(got, want) {
				t.Errorf("items = %v, want %v", got, want)
			}
			if page.TotalResults == nil || *page.TotalResults != 7 {
				t.Errorf("total_results = %v, want 7 source entries", page.TotalResults)
			}
			calls := seen()
			for _, required := range []string{"/find/ttfallback?page=&limit=", "/find/ttunknown?page=&limit="} {
				if !slices.Contains(calls, required) {
					t.Errorf("missing %s in TMDB requests %v", required, calls)
				}
			}
			for _, forbidden := range []string{"/find/ttmustnotresolve?page=&limit=", "/" + media.endpoint + "/0?page=&limit="} {
				if slices.Contains(calls, forbidden) {
					t.Errorf("unexpected TMDB request %s", forbidden)
				}
			}
		})
	}
}

func TestSeasonResumeSources(t *testing.T) {
	setCreds(t, "", "")
	const showID = 987701
	lib := uid.Get()
	mu := lib.GetMutex(uid.ShowsMutex)
	mu.Lock()
	previous := lib.Shows
	lib.Shows = append(slices.Clone(previous), &uid.Show{
		UIDs:     &uid.UniqueIDs{TMDB: showID},
		Episodes: []*uid.Episode{{Season: 0, Episode: 1, Resume: &uid.Resume{Position: 30, Total: 100}}},
	})
	mu.Unlock()
	t.Cleanup(func() { mu.Lock(); lib.Shows = previous; mu.Unlock() })
	for episode, position := range map[int]float64{1: 70, 2: 40} {
		key := bittorrent.TMDBResumeKey(true, showID, 0, episode)
		if err := database.GetCache().SetCachedObject(database.CommonBucket, 3600, key, &uid.Resume{Position: position, Total: 100}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { database.GetCache().Delete(database.CommonBucket, key) })
	}
	stubAPI(t, reqapi.TMDBAPI, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/987701/season/0" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"id":987702,"season_number":0,"episodes":[`)
		for episode := 1; episode <= 3; episode++ {
			if episode > 1 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%d,"season_number":0,"episode_number":%d,"name":"Special","overview":"Overview"}`, episode, episode)
		}
		fmt.Fprint(w, "]}")
	})
	w := do(newEngine(), request{path: "/api/shows/987701/seasons/0"})
	var season jsonapi.Season
	if err := json.Unmarshal(w.Body.Bytes(), &season); err != nil || w.Code != http.StatusOK || len(season.Episodes) != 3 {
		t.Fatalf("season: status %d, %v: %s", w.Code, err, w.Body)
	}
	for i, want := range []*float32{new(float32(30)), new(float32(40)), nil} {
		if got := season.Episodes[i].ResumeS; !reflect.DeepEqual(got, want) {
			t.Errorf("episode %d resume = %v, want %v", i+1, got, want)
		}
	}
}
