package reqapi_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/goccy/go-json"
)

const movieJSON = `{"id":550,"title":"Fight Club","original_title":"Fight Club","imdb_id":"tt0137523",
"popularity":61.416,"runtime":139,"release_date":"1999-10-15","vote_average":8.4,"vote_count":26280,
"genres":[{"id":18,"name":"Drama"}],"production_companies":[{"id":508,"name":"Regency","logo_path":null}],
"spoken_languages":[],"alternative_titles":{"titles":[{"iso_3166_1":"US","title":"Fight Club 1999"}]},
"release_dates":{"results":[{"iso_3166_1":"US","release_dates":[{"certification":"R","type":3}]}]}}`

// fetched simulates a request that got body from network and decoded it into result
func fetched(t testing.TB, result any, body string, status int) *reqapi.Request {
	r := &reqapi.Request{Result: result, ResponseStatusCode: status, ResponseBody: bytes.NewBufferString(body)}
	if err := r.Unmarshal(bytes.NewBufferString(body)); err != nil {
		t.Fatal(err)
	}
	return r
}

func roundtrip(t testing.TB, result, fresh any, body string, status int) *reqapi.CacheEntry {
	data, err := fetched(t, result, body, status).MarshalCache()
	if err != nil {
		t.Fatal(err)
	}
	if err := reqapi.ReadCache(&reqapi.Request{Result: fresh}, data); err != nil {
		t.Fatal(err)
	}
	c := &reqapi.CacheEntry{}
	if _, err := c.UnmarshalMsg(data); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCacheMsgpPointerToPointer(t *testing.T) {
	var movie, cached *tmdb.Movie
	c := roundtrip(t, &movie, &cached, movieJSON, 200)
	if c.Result == nil || c.Body != nil {
		t.Fatalf("expected msgpack result instead of body, got result=%d body=%d bytes", len(c.Result), len(c.Body))
	}
	if cached == nil || cached.Title != "Fight Club" || cached.ReleaseDates.Results[0].ReleaseDates[0].Certification != "R" {
		t.Fatalf("bad cached movie: %+v", cached)
	}
	// JSON keeps empty arrays non-nil, msgpack decodes them as nil
	movie.SpokenLanguages = nil
	if !reflect.DeepEqual(movie, cached) {
		t.Fatalf("cached movie differs:\n%+v\n%+v", movie, cached)
	}
}

func TestCacheMsgpNamedSlice(t *testing.T) {
	var movies, cached tmdb.Movies
	c := roundtrip(t, &movies, &cached, "["+movieJSON+"]", 200)
	if c.Result == nil || len(cached) != 1 || cached[0].IMDBId != "tt0137523" {
		t.Fatalf("bad cached movies: %+v", cached)
	}
}

func TestCacheJSONFallback(t *testing.T) {
	var shows, cached []*trakt.HiddenShow
	c := roundtrip(t, &shows, &cached, `[{"type":"show","show":{"title":"Lost","year":2004}}]`, 200)
	if c.Result != nil || c.Body == nil || len(cached) != 1 || cached[0].Show.Title != "Lost" {
		t.Fatalf("expected JSON body fallback, got %+v", c)
	}
}

func TestCacheNotFoundKeepsResultNil(t *testing.T) {
	var movie, cached *tmdb.Movie
	c := roundtrip(t, &movie, &cached, `{"status_code":34}`, 404)
	if c.Result != nil || cached != nil {
		t.Fatalf("404 should not produce a result, got %+v", cached)
	}
}

func TestCacheOldJSONEntryIsMiss(t *testing.T) {
	old, _ := json.Marshal(map[string]any{"body": []byte(movieJSON), "statuscode": 200})
	var movie *tmdb.Movie
	if err := reqapi.ReadCache(&reqapi.Request{Result: &movie}, old); err == nil {
		t.Fatal("old JSON cache entry must be treated as a miss")
	}
}

func BenchmarkCacheReadJSON(b *testing.B) {
	data := []byte(movieJSON)
	for b.Loop() {
		var m *tmdb.Movie
		if err := json.Unmarshal(data, &m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCacheReadMsgp(b *testing.B) {
	var movie *tmdb.Movie
	fetched(b, &movie, movieJSON, 200)
	bts, _ := movie.MarshalMsg(nil)
	for b.Loop() {
		m := &tmdb.Movie{}
		if _, err := m.UnmarshalMsg(bts); err != nil {
			b.Fatal(err)
		}
	}
}
