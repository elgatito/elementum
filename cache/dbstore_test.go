package cache_test

import (
	"os"
	"reflect"
	"testing"
	"time"

	msgpack "github.com/vmihailenco/msgpack/v5"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/xbmc"
)

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "elementum-cache")
	db, err := database.InitCacheDB(&config.Configuration{Info: &xbmc.AddonInfo{Profile: dir}})
	if err != nil {
		panic(err)
	}
	code := m.Run()
	db.Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

func watched() []*trakt.WatchedMovie {
	return []*trakt.WatchedMovie{
		{Plays: 3, LastWatchedAt: time.Date(2024, 1, 1, 23, 30, 0, 0, time.UTC), Movie: &trakt.Movie{Title: "Fight Club"}},
		nil,
	}
}

func TestDBStoreMsgp(t *testing.T) {
	store := cache.NewDBStore()

	movies := watched()
	if err := store.Set("msgp", &movies, time.Hour); err != nil {
		t.Fatal(err)
	}
	if b, _ := store.GetBytes("msgp"); len(b) == 0 || b[0] != 0xc1 {
		t.Fatalf("expected msgp marker, got %x", b[:min(len(b), 4)])
	}
	var cached []*trakt.WatchedMovie
	if err := store.Get("msgp", &cached); err != nil || !reflect.DeepEqual(movies, cached) {
		t.Fatalf("cached movies differ (%v):\n%+v\n%+v", err, movies, cached)
	}

	// value without bindings keeps vmihailenco encoding
	if err := store.Set("plain", map[string]int{"a": 1}, time.Hour); err != nil {
		t.Fatal(err)
	}
	var m map[string]int
	if err := store.Get("plain", &m); err != nil || m["a"] != 1 {
		t.Fatalf("bad plain value %v: %v", m, err)
	}
	// msgp entry read into type without bindings is an error, not a silent empty value
	if err := store.Get("msgp", &m); err == nil {
		t.Fatal("expected error decoding msgp entry into map")
	}
}

func TestDBStoreMsgpLayout(t *testing.T) {
	store := cache.NewDBStore()

	movies := watched()
	if err := store.Set("layout", &movies, time.Hour); err != nil {
		t.Fatal(err)
	}
	// WatchedShow shares "plays" and "last_watched_at" with WatchedMovie, so plain msgp decoding would silently succeed
	var shows []*trakt.WatchedShow
	if err := store.Get("layout", &shows); err == nil {
		t.Fatalf("expected layout mismatch error, got %+v", shows)
	}

	// pointer levels don't change layout
	movie := trakt.Movie{Title: "Fight Club"}
	if err := store.Set("pointer", movie, time.Hour); err != nil {
		t.Fatal(err)
	}
	var cached *trakt.Movie
	if err := store.Get("pointer", &cached); err != nil || cached.Title != "Fight Club" {
		t.Fatalf("bad cached movie (%v): %+v", err, cached)
	}
}

func TestDBStoreReadsOldEntry(t *testing.T) {
	store := cache.NewDBStore()

	movies := watched()
	old, err := msgpack.Marshal(cache.DBStoreItem{Key: "old", Value: &movies})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetBytes("old", old, time.Hour); err != nil {
		t.Fatal(err)
	}
	var cached []*trakt.WatchedMovie
	if err := store.Get("old", &cached); err != nil || len(cached) != 2 || cached[0].Movie.Title != "Fight Club" || !cached[0].LastWatchedAt.Equal(movies[0].LastWatchedAt) {
		t.Fatalf("bad old entry (%v): %+v", err, cached)
	}
}
