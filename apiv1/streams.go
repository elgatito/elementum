package apiv1

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

// Stream search follows the Kodi links routes (api.MovieLinks, api.ShowEpisodeLinks): same TMDB lookup,
// cache keys, providers and sorting, but the silent search, so Kodi shows no dialogs.
//
// shortcut: the provider search takes no context, so a client cancel only skips a search not started yet and a
// running one ends at the provider timeout; thread ctx into AddonSearcher.callRaw if abandoned searches pile up.

var (
	providersUnavailable = newError(ErrorCodeProvidersUnavailable, "Kodi or the search providers are not reachable")
	badPathID            = newError(ErrorCodeBadRequest, "tmdb_id and episode must be 1 or more, season 0 or more")
)

// ListMovieStreams searches the providers for a movie.
func (Server) ListMovieStreams(ctx context.Context, req ListMovieStreamsRequestObject) (ListMovieStreamsResponseObject, error) {
	if req.TmdbId < 1 {
		return ListMovieStreams400JSONResponse{BadRequestJSONResponse(badPathID)}, nil
	}
	c := ctx.(*gin.Context)
	key := strconv.Itoa(req.TmdbId)
	if cached := cachedStreams(key); cached != nil {
		return ListMovieStreams200JSONResponse(cached), nil
	}
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(c)
	if xbmcHost == nil {
		return ListMovieStreams502JSONResponse{ProvidersUnavailableJSONResponse(providersUnavailable)}, nil
	}

	// nil also when TMDB can't be reached; the fetcher doesn't tell the two apart.
	movie := tmdb.GetMovie(req.TmdbId, config.Get().Language)
	if movie == nil {
		return ListMovieStreams404JSONResponse{NotFoundJSONResponse(newError(ErrorCodeNotFound, "movie not found"))}, nil
	}
	searchers := providers.GetMovieSearchers(xbmcHost, providers.CallbackHost(c, xbmcHost))
	if len(searchers) == 0 {
		return ListMovieStreams502JSONResponse{ProvidersUnavailableJSONResponse(providersUnavailable)}, nil
	}
	if c.Request.Context().Err() != nil {
		return ListMovieStreams200JSONResponse{}, nil // client is gone, don't start the search
	}
	return ListMovieStreams200JSONResponse(storeStreams(key, providers.SearchMovieSilent(xbmcHost, searchers, movie, true))), nil
}

// ListEpisodeStreams searches the providers for an episode.
func (Server) ListEpisodeStreams(ctx context.Context, req ListEpisodeStreamsRequestObject) (ListEpisodeStreamsResponseObject, error) {
	if req.TmdbId < 1 || req.Season < 0 || req.Episode < 1 {
		return ListEpisodeStreams400JSONResponse{BadRequestJSONResponse(badPathID)}, nil
	}
	c := ctx.(*gin.Context)
	key := fmt.Sprintf("%d_%d_%d", req.TmdbId, req.Season, req.Episode)
	if cached := cachedStreams(key); cached != nil {
		return ListEpisodeStreams200JSONResponse(cached), nil
	}
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(c)
	if xbmcHost == nil {
		return ListEpisodeStreams502JSONResponse{ProvidersUnavailableJSONResponse(providersUnavailable)}, nil
	}

	notFound := ListEpisodeStreams404JSONResponse{NotFoundJSONResponse(newError(ErrorCodeNotFound, "episode not found"))}
	lang := config.Get().Language
	show := tmdb.GetShow(req.TmdbId, lang)
	if show == nil {
		return notFound, nil
	}
	season := tmdb.GetSeason(req.TmdbId, req.Season, lang, len(show.Seasons), true)
	if season == nil {
		return notFound, nil
	}
	episode := season.GetEpisode(req.Episode)
	if episode == nil {
		return notFound, nil
	}
	searchers := providers.GetEpisodeSearchers(xbmcHost, providers.CallbackHost(c, xbmcHost))
	if len(searchers) == 0 {
		return ListEpisodeStreams502JSONResponse{ProvidersUnavailableJSONResponse(providersUnavailable)}, nil
	}
	if c.Request.Context().Err() != nil {
		return ListEpisodeStreams200JSONResponse{}, nil // client is gone, don't start the search
	}
	return ListEpisodeStreams200JSONResponse(storeStreams(key, providers.SearchEpisodeSilent(xbmcHost, searchers, show, season, episode, true))), nil
}

// cachedStreams returns the cached search results for key, or nil when there are none.
func cachedStreams(key string) []Stream {
	if torrents, err := providers.GetCachedTorrents(key); err == nil && len(torrents) > 0 {
		return toStreams(torrents)
	}
	return nil
}

// storeStreams caches the results like the Kodi routes do (empty ones too) and maps them.
func storeStreams(key string, torrents []*bittorrent.TorrentFile) []Stream {
	if err := providers.SetCachedTorrents(key, torrents); err != nil {
		log.Warningf("Caching stream search for %s: %v", key, err)
	}
	return toStreams(torrents)
}

func toStreams(torrents []*bittorrent.TorrentFile) []Stream {
	streams := make([]Stream, 0, len(torrents)) // never null in JSON
	for _, t := range torrents {
		streams = append(streams, toStream(t))
	}
	return streams
}

// kodiTags matches Kodi label formatting; Burst sends provider names like "[COLOR FFF14E13]YTS[/COLOR]".
var kodiTags = regexp.MustCompile(`\[/?(COLOR[^\]]*|B|I)\]`)

func toStream(t *bittorrent.TorrentFile) Stream {
	s := Stream{
		Uri:           t.URI,
		Name:          t.Name,
		Provider:      kodiTags.ReplaceAllString(t.Provider, ""),
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
