package tmdb

import (
	"testing"

	"github.com/elgatito/elementum/config"
)

func TestGetManyPreservesOrder(t *testing.T) {
	got := getMany([]int{3, 1, 2}, func(id int) *int {
		v := id * 10
		return &v
	})

	want := []int{30, 10, 20}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}

	for i, v := range got {
		if v == nil || *v != want[i] {
			t.Fatalf("item %d = %v, want %d", i, v, want[i])
		}
	}
}

func TestGetUserLanguages(t *testing.T) {
	cfg := config.Get()
	oldL, oldS := cfg.Language, cfg.SecondLanguage
	t.Cleanup(func() { cfg.Language, cfg.SecondLanguage = oldL, oldS })
	cfg.Language, cfg.SecondLanguage = "en", ""

	for in, want := range map[string]string{"": "en,null", "en": "en,null", "de": "de,en,null", "de-DE": "de,en,null"} {
		if got := GetUserLanguages(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}

	// Kodi passes the configured language: the list (and so the cache key) is what it was before.
	cfg.Language, cfg.SecondLanguage = "de", "en"
	for in, want := range map[string]string{"": "de,en,null", "de": "de,en,null", "en": "en,null", "fr_FR": "fr,en,null"} {
		if got := GetUserLanguages(in); got != want {
			t.Errorf("de/en config, %q: %q, want %q", in, got, want)
		}
	}
}
