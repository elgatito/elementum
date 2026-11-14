package jsonapi

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/ui"
	"github.com/elgatito/elementum/xbmc"
)

// Stream search follows the Kodi links routes (api.MovieLinks, api.ShowEpisodeLinks): same TMDB lookup,
// cache keys, providers and sorting, but the silent search, so Kodi shows no dialogs.
//
// shortcut: the provider search takes no context, so a client cancel only skips a search not started yet and a
// running one ends at the provider timeout; thread ctx into AddonSearcher.callRaw if abandoned searches pile up.

var providersUnavailable = failed(http.StatusBadGateway, ErrorCodeProvidersUnavailable, "no search provider is available: built-in search is off and Kodi is not reachable")

// ListMovieStreams searches the providers for a movie.
func (Server) ListMovieStreams(c *gin.Context, id TmdbIdParam) {
	if id < 1 {
		fail(c, bad("tmdb_id must be 1 or more"))
		return
	}
	streams(c, strconv.Itoa(id), func(host *xbmc.XBMCHost, callback string) ([]*bittorrent.TorrentFile, *failure) {
		// nil also when TMDB can't be reached; the fetcher doesn't tell the two apart.
		movie := tmdb.GetMovie(id, config.Get().Language)
		if movie == nil {
			return nil, failed(http.StatusNotFound, ErrorCodeNotFound, "movie not found")
		}
		searchers := providers.GetMovieSearchers(host, callback)
		if len(searchers) == 0 {
			return nil, providersUnavailable
		}
		if c.Request.Context().Err() != nil {
			return nil, nil // client is gone, don't start the search
		}
		return providers.SearchMovieSilent(host, searchers, movie, true), nil
	})
}

// ListEpisodeStreams searches the providers for an episode.
func (Server) ListEpisodeStreams(c *gin.Context, id TmdbIdParam, seasonNumber SeasonParam, episodeNumber EpisodeParam) {
	if id < 1 || seasonNumber < 0 || episodeNumber < 1 {
		fail(c, bad("tmdb_id and episode must be 1 or more, season 0 or more"))
		return
	}
	streams(c, providers.EpisodeCacheKey(id, seasonNumber, episodeNumber), func(host *xbmc.XBMCHost, callback string) ([]*bittorrent.TorrentFile, *failure) {
		show, season, episode, err := tmdb.GetShowSeasonEpisode(id, seasonNumber, episodeNumber, config.Get().Language)
		if err != nil {
			return nil, failed(http.StatusNotFound, ErrorCodeNotFound, "episode not found")
		}
		searchers := providers.GetEpisodeSearchers(host, callback)
		if len(searchers) == 0 {
			return nil, providersUnavailable
		}
		if c.Request.Context().Err() != nil {
			return nil, nil // client is gone, don't start the search
		}
		return providers.SearchEpisodeSilent(host, searchers, show, season, episode, true), nil
	})
}

// streams answers the cached results for key, else what search finds (host is the request's Kodi, or nil). Results
// are cached like the Kodi routes do (empty ones too); a nil result without a failure is not.
func streams(c *gin.Context, key string, search func(host *xbmc.XBMCHost, callback string) ([]*bittorrent.TorrentFile, *failure)) {
	if torrents, err := providers.GetCachedTorrents(key); err == nil && len(torrents) > 0 {
		c.JSON(http.StatusOK, toStreams(torrents))
		return
	}
	// Without Kodi the built-in search still answers, unless providers_source turns it off.
	host, _ := xbmc.GetXBMCHostWithContext(c)
	if !providers.Available(host) {
		fail(c, providersUnavailable)
		return
	}
	torrents, f := search(host, providers.CallbackHost(c, host))
	if f != nil {
		fail(c, f)
		return
	}
	if torrents != nil {
		if err := providers.SetCachedTorrents(key, torrents); err != nil {
			log.Warningf("Caching stream search for %s: %v", key, err)
		}
	}
	c.JSON(http.StatusOK, toStreams(torrents))
}

func toStreams(torrents []*bittorrent.TorrentFile) []Stream {
	streams := make([]Stream, 0, len(torrents)) // never null in JSON
	for _, t := range torrents {
		streams = append(streams, toStream(t))
	}
	return streams
}

func toStream(t *bittorrent.TorrentFile) Stream {
	s := Stream{
		Uri:           t.URI,
		Name:          t.Name,
		Provider:      ui.Plain(t.Provider),
		Seeds:         int32(t.Seeds),
		Peers:         int32(t.Peers),
		MultiProvider: t.Multi,
		Resolution:    label[StreamResolution](bittorrent.Resolutions, t.Resolution),
		Rip:           label[StreamRip](bittorrent.Rips, t.RipType),
		Codec:         label[StreamCodec](bittorrent.Codecs, t.VideoCodec),
		Audio:         label[StreamAudio](bittorrent.Codecs, t.AudioCodec),
	}
	if t.InfoHash != "" {
		s.InfoHash = &t.InfoHash
	}
	if t.Language != "" {
		s.Language = &t.Language
	}
	if t.SizeParsed > 0 {
		size := int64(t.SizeParsed)
		s.SizeBytes = &size
	}
	return s
}

// label looks up an Elementum enum index in its name table; nil for unknown or out-of-spec values
// (Codecs is shared by video and audio).
func label[T interface {
	~string
	Valid() bool
}](table []string, i int) *T {
	if i <= 0 || i >= len(table) {
		return nil
	}
	if v := T(table[i]); v.Valid() {
		return &v
	}
	return nil
}
