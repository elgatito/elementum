package tmdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
)

func TestSplitCredits(t *testing.T) {
	b, _ := os.ReadFile("testdata/combined_credits.json")
	var raw combinedCredits
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	movies, shows := splitCredits(&raw, map[int]string{28: "Action", 878: "SF"}, map[int]string{10767: "Talk"})
	got := ids(movies)
	if want := []int{603, 604, 605}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("movies %v, want %v (no adult; deduped; cast first, then popularity)", got, want)
	}
	if len(movies[0].Genres) != 2 || movies[0].Genres[0].Name != "Action" || movies[0].GenreIDs != nil {
		t.Fatalf("genres not named: %+v", movies[0].Genres)
	}
	var sids []int
	for _, s := range shows {
		sids = append(sids, s.ID)
	}
	if len(sids) != 3 || sids[0] != 101 || sids[1] != 105 || sids[2] != 106 {
		t.Fatalf("shows %v, want [101 105 106] (talk, reality, self and genreless dropped; voice and crew kept)", sids)
	}
}

func stubCredits(t *testing.T, handler http.HandlerFunc) {
	srv := httptest.NewServer(handler)
	old := reqapi.TMDBAPI.Endpoint
	reqapi.TMDBAPI.Endpoint = srv.URL
	t.Cleanup(func() { reqapi.TMDBAPI.Endpoint = old; srv.Close() })
}

func TestPersonCreditsNotFound(t *testing.T) {
	stubCredits(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{"status_code":34}`, http.StatusNotFound) })
	if _, _, err := PersonCredits(1, "en"); !errors.Is(err, util.ErrNotFound) {
		t.Fatalf("err %v", err)
	}
}

func TestPersonCreditsEnglishOverview(t *testing.T) {
	stubCredits(t, func(w http.ResponseWriter, r *http.Request) {
		o := ""
		if r.URL.Query().Get("language") == "en-US" {
			o = "english"
		}
		fmt.Fprintf(w, `{"cast":[{"id":5,"media_type":"movie","title":"m","overview":%q}],"crew":[]}`, o)
	})
	movies, _, err := PersonCredits(2, "de-DE")
	if err != nil || len(movies) != 1 || movies[0].Overview != "english" {
		t.Fatalf("%v %+v", err, movies)
	}
}
