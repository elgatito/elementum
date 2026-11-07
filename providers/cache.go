package providers

import (
	"fmt"
	"os"
	"strings"

	"github.com/anacrolix/missinggo/perf"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
)

// GetCachedTorrents searches for torrent entries in the cache
func GetCachedTorrents(tmdbID string) ([]*bittorrent.TorrentFile, error) {
	defer perf.ScopeTimer()()

	if !config.Get().UseCacheSearch {
		return nil, fmt.Errorf("Caching is disabled")
	}

	cacheDB := database.GetCache()

	var ret []*bittorrent.TorrentFile
	err := cacheDB.GetCachedObject(database.CommonBucket, tmdbID, &ret)
	if len(ret) > 0 {
		for _, t := range ret {
			if !strings.HasPrefix(t.URI, "magnet:") {
				if _, err = os.Stat(t.URI); err != nil {
					return nil, fmt.Errorf("Cache is not up to date")
				}
			}
		}
	}

	return ret, err
}

// SetCachedTorrents caches torrent search results in cache
func SetCachedTorrents(tmdbID string, torrents []*bittorrent.TorrentFile) error {
	cacheDB := database.GetCache()

	return cacheDB.SetCachedObject(database.CommonBucket, config.Get().CacheSearchDuration, tmdbID, torrents)
}
