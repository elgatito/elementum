package uid

import (
	"fmt"

	"github.com/cespare/xxhash"
)

func NewLibraryContainer() *LibraryContainer {
	return &LibraryContainer{
		Items: map[uint64]struct{}{},
	}
}

func (c *LibraryContainer) Add(media MediaItemType, uids *UniqueIDs, ids ...int) {
	traktKey, tmdbKey, imdbKey := c.GetKeys(media, uids.Trakt, uids.TMDB, uids.IMDB, ids...)

	if traktKey != 0 {
		c.Items[traktKey] = struct{}{}
	}
	if tmdbKey != 0 {
		c.Items[tmdbKey] = struct{}{}
	}
	if imdbKey != 0 {
		c.Items[imdbKey] = struct{}{}
	}
}

func (c *LibraryContainer) Has(media MediaItemType, uids *UniqueIDs, ids ...int) bool {
	traktKey, tmdbKey, imdbKey := c.GetKeys(media, uids.Trakt, uids.TMDB, uids.IMDB, ids...)

	for item := range c.Items {
		if item == traktKey || item == tmdbKey || item == imdbKey {
			return true
		}
	}

	return false
}

func (c *LibraryContainer) Clear() {
	c.Items = map[uint64]struct{}{}
}

func (c *LibraryContainer) HasWithType(media MediaItemType, scraperType ScraperType, id int) bool {
	_, ok := c.Items[xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, scraperType, id))]

	return ok
}

func (c *LibraryContainer) GetKeys(media MediaItemType, traktID int, tmdbID int, imdbID string, ids ...int) (traktKey, tmdbKey, imdbKey uint64) {
	if media == MovieType {
		if traktID != 0 {
			traktKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TraktScraper, traktID))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TMDBScraper, tmdbID))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", media, IMDBScraper, imdbID))
		}
	} else if media == ShowType {
		if traktID != 0 {
			traktKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TraktScraper, traktID))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d", media, TMDBScraper, tmdbID))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s", media, IMDBScraper, imdbID))
		}
	} else if media == SeasonType && len(ids) > 0 {
		if traktID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", media, TraktScraper, traktID, ids[0]))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d", media, TMDBScraper, tmdbID, ids[0]))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s_%d", media, IMDBScraper, imdbID, ids[0]))
		}
	} else if media == EpisodeType && len(ids) > 0 {
		if traktID != 0 {
			traktKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", media, TraktScraper, traktID, ids[0], ids[1]))
		}
		if tmdbID != 0 {
			tmdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%d_%d_%d", media, TMDBScraper, tmdbID, ids[0], ids[1]))
		}
		if imdbID != "" {
			imdbKey = xxhash.Sum64String(fmt.Sprintf("%d_%d_%s_%d_%d", media, IMDBScraper, imdbID, ids[0], ids[1]))
		}
	}

	return
}
