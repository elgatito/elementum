package api

import (
	"maps"
	"net/url"
	"slices"
	"testing"

	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/tmdb"
)

func TestAdvancedSearchParams(t *testing.T) {
	filters, _ := url.ParseQuery("year=2010+-+2000&genre=28&genre=12&country=US&country=GB&language=en&cast=287:Brad+Pitt&cast=1:A")
	want := napping.Params{
		"sort_by":                  "popularity.desc",
		"primary_release_date.gte": "2000-01-01",
		"primary_release_date.lte": "2010-12-31",
		"with_genres":              "28,12",
		"with_origin_country":      "US|GB",
		"with_original_language":   "en",
		"with_cast":                "287,1",
	}
	if got := advancedSearchParams("movies", filters); !maps.Equal(got, want) {
		t.Errorf("movies params = %v, want %v", got, want)
	}

	filters, _ = url.ParseQuery("year=1999&cast=287:Brad+Pitt&sort=newest")
	want = napping.Params{"sort_by": "first_air_date.desc", "first_air_date.gte": "1999-01-01", "first_air_date.lte": "1999-12-31"}
	if got := advancedSearchParams("shows", filters); !maps.Equal(got, want) {
		t.Errorf("shows params = %v, want %v", got, want)
	}

	filters, _ = url.ParseQuery("sort=newest")
	if got := advancedSearchParams("movies", filters); got["primary_release_date.lte"] == "" {
		t.Errorf("newest sort without year should cap release date at today, got %v", got)
	}

	for in, want := range map[string][2]int{
		"2010":        {2010, 2010},
		"2010 - 2000": {2000, 2010},
		"2010-":       {2010, 0},
		" - 2010":     {0, 2010},
	} {
		if from, to, ok := parseYearRange(in); !ok || from != want[0] || to != want[1] {
			t.Errorf("parseYearRange(%q) = %d, %d, %v, want %v", in, from, to, ok, want)
		}
	}

	for _, bad := range []string{"", "-", "99", "abcd", "2010-20", "2010 2015", "20102015"} {
		if _, _, ok := parseYearRange(bad); ok {
			t.Errorf("parseYearRange(%q) accepted invalid input", bad)
		}
	}

	filters, _ = url.ParseQuery("year=2010-&sort=newest")
	if got := advancedSearchParams("movies", filters); got["primary_release_date.gte"] != "2010-01-01" || got["primary_release_date.lte"] == "" {
		t.Errorf("open-ended year with newest sort = %v, want gte 2010-01-01 and lte capped at today", got)
	}
	filters, _ = url.ParseQuery("year=-2010")
	if got := advancedSearchParams("shows", filters); got["first_air_date.gte"] != "" || got["first_air_date.lte"] != "2010-12-31" {
		t.Errorf("up-to year = %v, want only lte 2010-12-31", got)
	}
}

func TestTitleSearch(t *testing.T) {
	movie := func(id int, title, date, country string, popularity float64) *tmdb.Movie {
		return &tmdb.Movie{
			Entity:              tmdb.Entity{ID: id, Title: title, ReleaseDate: date, OriginalLanguage: "en", Genres: []*tmdb.IDName{{ID: 28}}},
			ProductionCountries: []*tmdb.Country{{Iso31661: country}},
			Popularity:          popularity,
		}
	}
	pages := 0
	search := func(page int) ([]*tmdb.Movie, int) {
		pages++
		return []*tmdb.Movie{
			movie(1, "Deadpool", "2016-02-09", "US", 50),
			nil,
			movie(2, "deadpool 2", "2018-05-15", "US", 80),
			movie(3, "Deadpool Fan Film", "2009-01-01", "CA", 1),
		}, 4
	}
	ids := func(movies []*tmdb.Movie) (ret []int) {
		for _, m := range movies {
			ret = append(ret, m.ID)
		}
		return
	}

	for query, want := range map[string][]int{
		"year=2010-":            {2, 1},
		"year=2010-&sort=title": {1, 2},
		"sort=oldest":           {3, 1, 2},
		"country=CA&country=GB": {3},
		"genre=28&genre=35":     nil,
		"language=fr":           nil,
		"cast=287:Brad+Pitt":    nil,
	} {
		filters, _ := url.ParseQuery(query)
		got, total := titleSearch(search, movieSearchItem, advancedSearchParams("movies", filters), 1, 20)
		if !slices.Equal(ids(got), want) || total != len(want) {
			t.Errorf("titleSearch(%q) = %v (total %d), want %v", query, ids(got), total, want)
		}
	}
	if pages != 7 {
		t.Errorf("search was called %d times, want one page per query", pages)
	}

	got, total := titleSearch(search, movieSearchItem, advancedSearchParams("movies", url.Values{}), 2, 2)
	if !slices.Equal(ids(got), []int{3}) || total != 3 {
		t.Errorf("second page = %v (total %d), want [3] of 3", ids(got), total)
	}
}
