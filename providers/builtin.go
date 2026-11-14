package providers

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers/burst"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

// Where torrents are searched, the providers_source setting.
const (
	SourceAuto    = iota // built-in search without Kodi, Kodi provider add-ons with it
	SourceBuiltin        // Elementum's own search (providers/burst)
	SourceKodi           // Kodi add-ons named script.elementum.*
	SourceBoth
)

func init() { burst.Client = proxy.GetProxyClient }

// builtinSearcher is Elementum's own search behind the Searcher interfaces. It builds the same search
// objects the Kodi provider add-ons get, so both search for the same titles and years.
type builtinSearcher struct{ objects *AddonSearcher }

// useBuiltin and useKodi tell which searchers the providers_source setting asks for.
func useBuiltin(xbmcHost *xbmc.XBMCHost) bool {
	s := config.Get().ProvidersSource
	return s == SourceBuiltin || s == SourceBoth || (s == SourceAuto && xbmcHost == nil)
}

func useKodi(xbmcHost *xbmc.XBMCHost) bool {
	return xbmcHost != nil && config.Get().ProvidersSource != SourceBuiltin
}

// Available reports whether there is anything to search with: the built-in search, or a reachable Kodi.
func Available(xbmcHost *xbmc.XBMCHost) bool { return useBuiltin(xbmcHost) || useKodi(xbmcHost) }

// ProvidersDir is the folder of the user's provider files, in the profile.
func ProvidersDir() string {
	if info := config.Get().Info; info != nil && info.Profile != "" {
		return filepath.Join(info.Profile, "providers")
	}
	return ""
}

func (b builtinSearcher) search(q burst.Query) []*bittorrent.TorrentFile {
	ctx, cancel := context.WithTimeout(context.Background(), SearchTimeout())
	defer cancel()
	for k, v := range q.Titles { // definitions name languages in lower case
		if l := strings.ToLower(k); l != k {
			q.Titles[l] = v
		}
	}
	q.Languages = []string{strings.ToLower(strings.SplitN(config.Get().Language, "-", 2)[0])}
	if q.Languages[0] != "en" {
		q.Languages = append(q.Languages, "en")
	}
	// Through JSON, as add-on results come: decoding fills resolution, codecs and size of a TorrentFile.
	torrents := make([]*bittorrent.TorrentFile, 0)
	if b, err := json.Marshal(burst.Search(ctx, ProvidersDir(), q)); err != nil {
		log.Errorf("Failed to encode built-in results: %s", err)
	} else if err := json.Unmarshal(b, &torrents); err != nil {
		log.Errorf("Failed to decode built-in results: %s", err)
	}
	return torrents
}

func (b builtinSearcher) SearchLinks(query string) []*bittorrent.TorrentFile {
	return b.search(burst.Query{Method: "general", Title: query, Titles: map[string]string{"source": query, "original": query}})
}

func (b builtinSearcher) SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile {
	o := b.objects.GetMovieSearchObject(movie)
	return b.search(burst.Query{Method: "movie", Title: o.Title, Titles: o.Titles, Year: o.Year, Years: o.Years, IMDBId: o.IMDBId, TMDBId: o.TMDBId})
}

func (b builtinSearcher) SearchMovieLinksSilent(movie *tmdb.Movie, _ bool) []*bittorrent.TorrentFile {
	return b.SearchMovieLinks(movie) // the built-in search has no dialogs and no logins yet
}

func (b builtinSearcher) SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile {
	o := b.objects.GetSeasonSearchObject(show, season)
	return b.search(burst.Query{Method: "season", Title: o.Title, Titles: o.Titles, Year: o.Year, Season: o.Season,
		IMDBId: o.IMDBId, TMDBId: o.TMDBId, ShowTMDBId: o.ShowTMDBId, TVDBId: o.TVDBId})
}

func (b builtinSearcher) SearchEpisodeLinks(show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode) []*bittorrent.TorrentFile {
	o := b.objects.GetEpisodeSearchObject(show, season, episode)
	method := "episode"
	if o.Anime {
		method = "anime"
	}
	return b.search(burst.Query{Method: method, Title: o.Title, Titles: o.Titles, Year: o.Year, Season: o.Season, Episode: o.Episode,
		AbsoluteNumber: o.AbsoluteNumber, IMDBId: o.IMDBId, TMDBId: o.TMDBId, ShowTMDBId: o.ShowTMDBId, TVDBId: o.TVDBId})
}

func (b builtinSearcher) SearchEpisodeLinksSilent(show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode, _ bool) []*bittorrent.TorrentFile {
	return b.SearchEpisodeLinks(show, season, episode)
}
