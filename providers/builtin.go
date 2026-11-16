package providers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers/burst"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/ui"
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
	info := config.Get().Info
	if info == nil || info.Profile == "" {
		return ""
	}
	return filepath.Join(filepath.Clean(info.Profile), "providers")
}

var importMu sync.Mutex

// BurstSettingsPath is the settings file of the Burst add-on: Kodi keeps every add-on's settings next to
// each other in addon_data. "" without a profile.
func BurstSettingsPath() string {
	dir := ProvidersDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(dir)), "script.elementum.burst", "settings.xml")
}

// ImportBurstSettings takes over the settings of the Burst add-on and tells Kodi how it went.
func ImportBurstSettings(xbmcHost *xbmc.XBMCHost) {
	if enabled, imported := burst.ImportBurst(ProvidersDir(), BurstSettingsPath()); imported {
		xbmcHost.Notify("Elementum", fmt.Sprintf(ui.Text(ui.LanguageOf(xbmcHost), "Burst settings imported, %d providers enabled"), enabled), config.AddonIcon())
	}
}

// OfferBurstImport asks in Kodi, on the first run of the built-in search, whether to take over the
// settings of the Burst add-on: enabled providers, logins, mirrors, filters and the cookie sync. The
// answer is final (closing the dialog counts as no): afterwards the built-in search has its own settings
// file. Without a Kodi to ask nothing happens, and it is offered on the next search from Kodi.
func OfferBurstImport(xbmcHost *xbmc.XBMCHost) {
	dir := ProvidersDir()
	if xbmcHost == nil || dir == "" {
		return
	}
	importMu.Lock() // two searches at once must not ask twice
	defer importMu.Unlock()
	if !burst.CanImportBurst(dir, BurstSettingsPath()) {
		return
	}
	lang := ui.LanguageOf(xbmcHost)
	if !xbmcHost.DialogConfirmNonTimed("Elementum", ui.Text(lang, "Import settings of the Burst add-on into the built-in search?")) {
		if err := burst.SkipImport(dir); err != nil {
			log.Warningf("Could not create the built-in search settings: %s", err)
		}
		return
	}
	ImportBurstSettings(xbmcHost)
}

// search runs the built-in search. A search that is not silent shows its progress in Kodi, when the
// request came from one.
func (b builtinSearcher) search(q burst.Query, silent bool) []*bittorrent.TorrentFile {
	ctx, cancel := context.WithTimeout(context.Background(), SearchTimeout())
	defer cancel()
	var progress func(done, total int, waiting []string)
	if host := b.objects.xbmcHost; host != nil && !silent {
		OfferBurstImport(host)
		lang := ui.LanguageOf(host)
		if dialog := host.NewDialogProgressBG("Elementum", ui.Text(lang, "Built-in search")); dialog != nil {
			defer dialog.Close()
			progress = func(done, total int, waiting []string) {
				message := fmt.Sprintf(ui.Text(lang, "Searching: %s"), strings.Join(waiting, ", "))
				if len(waiting) > 4 {
					message = fmt.Sprintf(ui.Text(lang, "%d providers left"), len(waiting))
				}
				dialog.Update(done*100/max(total, 1), "Elementum", message)
			}
		}
	}
	for k, v := range q.Titles { // definitions name languages in lower case
		if l := strings.ToLower(k); l != k {
			q.Titles[l] = v
		}
	}
	q.Language = strings.ToLower(strings.SplitN(config.Get().Language, "-", 2)[0])
	// Through JSON, as add-on results come: decoding fills resolution, codecs and size of a TorrentFile.
	torrents := make([]*bittorrent.TorrentFile, 0)
	if b, err := json.Marshal(burst.Search(ctx, ProvidersDir(), q, progress)); err != nil {
		log.Errorf("Failed to encode built-in results: %s", err)
	} else if err := json.Unmarshal(b, &torrents); err != nil {
		log.Errorf("Failed to decode built-in results: %s", err)
	}
	for _, t := range torrents {
		t.Icon = config.KodiPath(t.Icon) // Kodi on Windows does not open the /mnt/c/... of Elementum in WSL
	}
	return torrents
}

func (b builtinSearcher) SearchLinks(query string) []*bittorrent.TorrentFile {
	return b.search(burst.Query{Method: "general", Title: query, Titles: map[string]string{"source": query, "original": query}}, false)
}

func (b builtinSearcher) SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile {
	o := b.objects.GetMovieSearchObject(movie)
	return b.search(burst.Query{Method: "movie", Title: o.Title, Titles: o.Titles, Year: o.Year, Years: o.Years, IMDBId: o.IMDBId, TMDBId: o.TMDBId}, false)
}

// The built-in search has no dialogs, so a silent search differs only in signing in to private trackers.
func (b builtinSearcher) SearchMovieLinksSilent(movie *tmdb.Movie, withAuth bool) []*bittorrent.TorrentFile {
	o := b.objects.GetMovieSearchObject(movie)
	return b.search(burst.Query{Method: "movie", Title: o.Title, Titles: o.Titles, Year: o.Year, Years: o.Years, IMDBId: o.IMDBId, TMDBId: o.TMDBId, SkipAuth: !withAuth}, true)
}

func (b builtinSearcher) SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile {
	o := b.objects.GetSeasonSearchObject(show, season)
	return b.search(burst.Query{Method: "season", Title: o.Title, Titles: o.Titles, Year: o.Year, Season: o.Season,
		IMDBId: o.IMDBId, TMDBId: o.TMDBId, ShowTMDBId: o.ShowTMDBId, TVDBId: o.TVDBId}, false)
}

func (b builtinSearcher) SearchEpisodeLinks(show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode) []*bittorrent.TorrentFile {
	return b.searchEpisode(show, season, episode, true, false)
}

func (b builtinSearcher) SearchEpisodeLinksSilent(show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode, withAuth bool) []*bittorrent.TorrentFile {
	return b.searchEpisode(show, season, episode, withAuth, true)
}

func (b builtinSearcher) searchEpisode(show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode, withAuth, silent bool) []*bittorrent.TorrentFile {
	o := b.objects.GetEpisodeSearchObject(show, season, episode)
	method := "episode"
	if o.Anime {
		method = "anime"
	}
	return b.search(burst.Query{Method: method, Title: o.Title, Titles: o.Titles, Year: o.Year, Season: o.Season, Episode: o.Episode,
		AbsoluteNumber: o.AbsoluteNumber, IMDBId: o.IMDBId, TMDBId: o.TMDBId, ShowTMDBId: o.ShowTMDBId, TVDBId: o.TVDBId, SkipAuth: !withAuth}, silent)
}
