package database

import (
	"path/filepath"
	"testing"

	"github.com/asdine/storm/v3"
	"github.com/asdine/storm/v3/q"
)

func TestMigrateLibraryItems(t *testing.T) {
	db, err := storm.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	{
		// Old schema, same name to use the same bucket
		type LibraryItem struct {
			ID        int `storm:"id"`
			MediaType int `storm:"index"`
			State     int `storm:"index"`
			ShowID    int `storm:"index"`
		}
		for _, li := range []LibraryItem{{ID: 10, MediaType: 0, State: 0}, {ID: 20, MediaType: 1, State: 1, ShowID: 20}} {
			if err := db.Save(&li); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := migrateLibraryItems(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateLibraryItems(db); err != nil {
		t.Fatal(err)
	}

	var li LibraryItem
	if err := db.One("Key", LibraryItemKey(0, 10), &li); err != nil || li.ID != 10 || li.State != 0 {
		t.Fatalf("migrated movie: %+v, %v", li, err)
	}

	// A show with the same TMDB ID as a movie must not overwrite it
	if err := db.Save(&LibraryItem{Key: LibraryItemKey(1, 10), ID: 10, MediaType: 1, State: 1, ShowID: 10}); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Select(q.Eq("ID", 10)).Count(&LibraryItem{}); n != 2 {
		t.Fatalf("expected movie and show with ID 10, got %d", n)
	}
	if n, _ := db.Select(q.Eq("ID", 20), q.Eq("MediaType", 1), q.Eq("State", 1)).Count(&LibraryItem{}); n != 1 {
		t.Fatalf("migrated show not found by ID, got %d", n)
	}
}
