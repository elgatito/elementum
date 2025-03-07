package providers

import (
	"fmt"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/sync"
	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/ip"
	"github.com/elgatito/elementum/xbmc"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
)

const (
	// if >= 80% of episodes have absolute numbers, assume it's because we need it
	mixAbsoluteNumberPercentage = 0.8
)

// AddonSearcher ...
type AddonSearcher struct {
	MovieSearcher
	SeasonSearcher
	EpisodeSearcher

	addonID      string
	callbackHost string
	xbmcHost     *xbmc.XBMCHost
	log          *logging.Logger
}

var cbLock = sync.RWMutex{}
var callbacks = map[string]chan []byte{}

// GetCallback ...
func GetCallback() (string, chan []byte) {
	cid := strconv.Itoa(rand.Int())
	c := make(chan []byte, 1) // make sure we don't block clients when we write on it
	cbLock.Lock()
	callbacks[cid] = c
	cbLock.Unlock()
	return cid, c
}

// RemoveCallback ...
func RemoveCallback(cid string) {
	cbLock.Lock()
	defer cbLock.Unlock()

	delete(callbacks, cid)
}

// CallbackHandler ...
func CallbackHandler(ctx *gin.Context) {
	cid := ctx.Params.ByName("cid")
	cbLock.RLock()
	c, ok := callbacks[cid]
	cbLock.RUnlock()
	// maybe the callback was already removed because we were too slow,
	// it's fine.
	if !ok {
		return
	}
	RemoveCallback(cid)
	body, _ := io.ReadAll(ctx.Request.Body)
	c <- body
	close(c)
}

func getSearchers(xbmcHost *xbmc.XBMCHost, callbackHost string) []interface{} {
	list := make([]interface{}, 0)
	for _, addon := range xbmcHost.GetAddons("xbmc.python.script", "executable", true).Addons {
		if strings.HasPrefix(addon.ID, "script.elementum.") {
			list = append(list, NewAddonSearcher(xbmcHost, callbackHost, addon.ID))
		}
	}
	return list
}

// GetMovieSearchers ...
func GetMovieSearchers(xbmcHost *xbmc.XBMCHost, callbackHost string) []MovieSearcher {
	searchers := make([]MovieSearcher, 0)
	for _, searcher := range getSearchers(xbmcHost, callbackHost) {
		searchers = append(searchers, searcher.(MovieSearcher))
	}
	return searchers
}

// GetSeasonSearchers ...
func GetSeasonSearchers(xbmcHost *xbmc.XBMCHost, callbackHost string) []SeasonSearcher {
	searchers := make([]SeasonSearcher, 0)
	for _, searcher := range getSearchers(xbmcHost, callbackHost) {
		searchers = append(searchers, searcher.(SeasonSearcher))
	}
	return searchers
}

// GetEpisodeSearchers ...
func GetEpisodeSearchers(xbmcHost *xbmc.XBMCHost, callbackHost string) []EpisodeSearcher {
	searchers := make([]EpisodeSearcher, 0)
	for _, searcher := range getSearchers(xbmcHost, callbackHost) {
		searchers = append(searchers, searcher.(EpisodeSearcher))
	}
	return searchers
}

// GetSearchers ...
func GetSearchers(xbmcHost *xbmc.XBMCHost, callbackHost string) []Searcher {
	searchers := make([]Searcher, 0)
	for _, searcher := range getSearchers(xbmcHost, callbackHost) {
		searchers = append(searchers, searcher.(Searcher))
	}
	return searchers
}

// NewAddonSearcher ...
func NewAddonSearcher(xbmcHost *xbmc.XBMCHost, callbackHost string, addonID string) *AddonSearcher {
	if callbackHost == "" {
		if config.Args.LocalHost != "" {
			callbackHost = fmt.Sprintf("%s:%d", config.Args.LocalHost, config.Args.LocalPort)
		} else {
			callbackHost = fmt.Sprintf("%s:%d", "127.0.0.1", config.Args.LocalPort)
		}
	}

	return &AddonSearcher{
		xbmcHost:     xbmcHost,
		addonID:      addonID,
		callbackHost: callbackHost,
		log:          logging.MustGetLogger(fmt.Sprintf("AddonSearcher %s", addonID)),
	}
}

// GetQuerySearchObject ...
func (as *AddonSearcher) GetQuerySearchObject(query string) *QuerySearchObject {
	sObject := &QuerySearchObject{
		Query: query,
	}

	sObject.ProxyURL = config.Get().ProxyURL
	sObject.ElementumURL = ip.ElementumURL(as.xbmcHost)
	sObject.InternalProxyURL = ip.InternalProxyURL(as.xbmcHost)

	return sObject
}

// GetMovieSearchSilentObject ...
func (as *AddonSearcher) GetMovieSearchSilentObject(movie *tmdb.Movie, withAuth bool) *MovieSearchObject {
	o := as.GetMovieSearchObject(movie)
	o.Silent = true
	o.SkipAuth = !withAuth

	return o
}

// GetMovieSearchObject ...
func (as *AddonSearcher) GetMovieSearchObject(movie *tmdb.Movie) *MovieSearchObject {
	year, _ := strconv.Atoi(strings.Split(movie.ReleaseDate, "-")[0])
	title := movie.OriginalTitle

	// Iterate through all available dates and take the earliest one as a basic date for searching
	if config.Get().ProviderUseLowestReleaseDate && movie.ReleaseDates != nil && movie.ReleaseDates.Results != nil {
		for _, r := range movie.ReleaseDates.Results {
			if r.ReleaseDates == nil {
				continue
			}

			for _, d := range r.ReleaseDates {
				y, _ := strconv.Atoi(strings.Split(d.ReleaseDate, "-")[0])
				if y < year {
					year = y
				}
			}
		}
	}

	sObject := &MovieSearchObject{
		IMDBId: movie.IMDBId,
		TMDBId: movie.ID,
		Title:  NormalizeTitle(title),
		Year:   year,
		Years: map[string]int{
			"original": year,
		},
		Titles: map[string]string{
			"original": NormalizeTitle(movie.OriginalTitle),
			"source":   movie.OriginalTitle,
		},
	}

	// Collect release dates per each location
	if movie.ReleaseDates != nil && movie.ReleaseDates.Results != nil {
		for _, r := range movie.ReleaseDates.Results {
			if r.ReleaseDates == nil {
				continue
			}

			lowestYear := 0
			for _, d := range r.ReleaseDates {
				if d == nil {
					continue
				}

				y, _ := strconv.Atoi(strings.Split(d.ReleaseDate, "-")[0])
				if y < lowestYear || lowestYear == 0 {
					lowestYear = y
				}
			}

			if lowestYear > 0 {
				sObject.Years[strings.ToLower(r.Iso3166_1)] = lowestYear
			}
		}
	}

	// Collect titles from AlternativeTitles
	var romaji string
	if movie.AlternativeTitles != nil && movie.AlternativeTitles.Titles != nil {
		for _, title := range movie.AlternativeTitles.Titles {
			lang := strings.ToLower(title.Iso3166_1)
			sObject.Titles[lang] = NormalizeTitle(title.Title)
			if lang == "us" {
				sObject.Titles["en"] = sObject.Titles[lang]
			}
			// try to find title in Romaji for anime
			if lang == "jp" && strings.ToLower(title.Type) == "romaji" {
				romaji = sObject.Titles[lang]
			}
		}
	}
	if romaji != "" {
		sObject.Titles["jp"] = romaji
	}

	// Collect titles from Translations
	if movie.Translations != nil && movie.Translations.Translations != nil {
		for _, tr := range movie.Translations.Translations {
			if tr.Data == nil || tr.Data.Title == "" {
				continue
			}

			sObject.Titles[strings.ToLower(tr.Iso3166_1)] = NormalizeTitle(tr.Data.Title)
			sObject.Titles[strings.ToLower(tr.Iso639_1)] = NormalizeTitle(tr.Data.Title)
		}
	}

	sObject.Titles[strings.ToLower(movie.OriginalLanguage)] = NormalizeTitle(sObject.Titles["source"])
	sObject.Titles[strings.ToLower(config.Get().Language)] = NormalizeTitle(movie.GetTitle())

	sObject.ProxyURL = config.Get().ProxyURL
	sObject.ElementumURL = ip.ElementumURL(as.xbmcHost)
	sObject.InternalProxyURL = ip.InternalProxyURL(as.xbmcHost)

	return sObject
}

// GetSeasonSearchObject ...
func (as *AddonSearcher) GetSeasonSearchObject(show *tmdb.Show, season *tmdb.Season) *SeasonSearchObject {
	year, _ := strconv.Atoi(strings.Split(season.AirDate, "-")[0])
	title := show.OriginalName

	sObject := &SeasonSearchObject{
		IMDBId:     show.ExternalIDs.IMDBId,
		TVDBId:     util.StrInterfaceToInt(show.ExternalIDs.TVDBID),
		ShowTMDBId: show.ID,
		Title:      NormalizeTitle(title),
		Titles:     map[string]string{"original": NormalizeTitle(show.OriginalName), "source": show.OriginalName},
		Year:       year,
		Season:     season.Season,
		Anime:      show.IsAnime(),
	}

	// Collect titles from AlternativeTitles
	var romaji string
	if show.AlternativeTitles != nil && show.AlternativeTitles.Titles != nil {
		for _, title := range show.AlternativeTitles.Titles {
			lang := strings.ToLower(title.Iso3166_1)
			sObject.Titles[lang] = NormalizeTitle(title.Title)
			if lang == "us" {
				sObject.Titles["en"] = sObject.Titles[lang]
			}
			// try to find title in Romaji for anime
			if lang == "jp" && strings.ToLower(title.Type) == "romaji" {
				romaji = sObject.Titles[lang]
			}
		}
	}
	if romaji != "" {
		sObject.Titles["jp"] = romaji
	}

	// Collect titles from Translations
	if show.Translations != nil && show.Translations.Translations != nil {
		for _, tr := range show.Translations.Translations {
			if tr.Data == nil || tr.Data.Name == "" {
				continue
			}

			sObject.Titles[strings.ToLower(tr.Iso3166_1)] = NormalizeTitle(tr.Data.Name)
			sObject.Titles[strings.ToLower(tr.Iso639_1)] = NormalizeTitle(tr.Data.Name)
		}
	}

	sObject.Titles[strings.ToLower(show.OriginalLanguage)] = NormalizeTitle(sObject.Titles["source"])
	sObject.Titles[strings.ToLower(config.Get().Language)] = NormalizeTitle(show.GetName())

	sObject.ProxyURL = config.Get().ProxyURL
	sObject.ElementumURL = ip.ElementumURL(as.xbmcHost)
	sObject.InternalProxyURL = ip.InternalProxyURL(as.xbmcHost)

	return sObject
}

// GetEpisodeSearchObject ...
func (as *AddonSearcher) GetEpisodeSearchObject(show *tmdb.Show, episode *tmdb.Episode) *EpisodeSearchObject {
	if show == nil || episode == nil {
		return nil
	}

	seasonCurrent := show.GetSeasonByRealNumber(episode.SeasonNumber)
	seasonFirst := show.GetSeasonByRealNumber(1)

	seasonYear := 0
	showYear := 0

	year, _ := strconv.Atoi(strings.Split(episode.AirDate, "-")[0])

	if seasonCurrent != nil {
		seasonYear, _ = strconv.Atoi(strings.Split(seasonCurrent.AirDate, "-")[0])
	}
	if seasonFirst != nil {
		showYear, _ = strconv.Atoi(strings.Split(seasonFirst.AirDate, "-")[0])
	}

	title := show.OriginalName

	seasonName := ""
	if s := show.GetSeasonByRealNumber(episode.SeasonNumber); s != nil {
		seasonName = s.Name
	}

	tvdbID := util.StrInterfaceToInt(show.ExternalIDs.TVDBID)

	// Some Torrents use absolute episodes range in name.
	// Provider can use Absolute number to filter such.
	absoluteNumber := 0
	episodesTillSeason := show.EpisodesTillSeason(episode.SeasonNumber)
	if episodesTillSeason > 0 && episodesTillSeason < episode.EpisodeNumber {
		absoluteNumber = episode.EpisodeNumber
		episode.EpisodeNumber = episode.EpisodeNumber - episodesTillSeason
	} else if tvdbID > 0 {
		an, st := show.ShowInfo(episode)

		if an != 0 {
			absoluteNumber = an
		}
		if st != "" {
			title = st
		}
	}

	sObject := &EpisodeSearchObject{
		IMDBId:         show.ExternalIDs.IMDBId,
		TVDBId:         tvdbID,
		TMDBId:         episode.ID,
		ShowTMDBId:     show.ID,
		Title:          NormalizeTitle(title),
		Titles:         map[string]string{"original": NormalizeTitle(show.OriginalName), "source": show.OriginalName},
		Season:         episode.SeasonNumber,
		SeasonName:     seasonName,
		Episode:        episode.EpisodeNumber,
		Year:           year,
		SeasonYear:     seasonYear,
		ShowYear:       showYear,
		AbsoluteNumber: absoluteNumber,
		Anime:          show.IsAnime(),
	}

	// Collect titles from AlternativeTitles
	var romaji string
	if show.AlternativeTitles != nil && show.AlternativeTitles.Titles != nil {
		for _, title := range show.AlternativeTitles.Titles {
			lang := strings.ToLower(title.Iso3166_1)
			sObject.Titles[lang] = NormalizeTitle(title.Title)
			if lang == "us" {
				sObject.Titles["en"] = sObject.Titles[lang]
			}
			// try to find title in Romaji for anime
			if lang == "jp" && strings.ToLower(title.Type) == "romaji" {
				romaji = sObject.Titles[lang]
			}
		}
	}
	if romaji != "" {
		sObject.Titles["jp"] = romaji
	}

	// Collect titles from Translations
	if show.Translations != nil && show.Translations.Translations != nil {
		for _, tr := range show.Translations.Translations {
			if tr.Data == nil || tr.Data.Name == "" {
				continue
			}

			sObject.Titles[strings.ToLower(tr.Iso3166_1)] = NormalizeTitle(tr.Data.Name)
			sObject.Titles[strings.ToLower(tr.Iso639_1)] = NormalizeTitle(tr.Data.Name)
		}
	}

	sObject.Titles[strings.ToLower(show.OriginalLanguage)] = NormalizeTitle(sObject.Titles["source"])
	sObject.Titles[strings.ToLower(config.Get().Language)] = NormalizeTitle(show.GetName())

	sObject.ProxyURL = config.Get().ProxyURL
	sObject.ElementumURL = ip.ElementumURL(as.xbmcHost)
	sObject.InternalProxyURL = ip.InternalProxyURL(as.xbmcHost)

	return sObject
}

func (as *AddonSearcher) call(method string, searchObject interface{}) []*bittorrent.TorrentFile {
	torrents := make([]*bittorrent.TorrentFile, 0)
	cid, c := GetCallback()
	cbURL := fmt.Sprintf("http://%s/callbacks/%s", as.callbackHost, cid)

	payload := &SearchPayload{
		Method:           method,
		CallbackURL:      cbURL,
		CallbackLogin:    config.Args.LocalLogin,
		CallbackPassword: config.Args.LocalPassword,
		SearchObject:     searchObject,
	}

	as.xbmcHost.ExecuteAddon(as.addonID, payload.String())

	timeout := providerTimeout()
	if config.Get().CustomProviderTimeoutEnabled {
		timeout = time.Duration(config.Get().CustomProviderTimeout) * time.Second
	}

	select {
	case <-time.After(timeout):
		as.log.Warningf("Provider %s was too slow. Ignored.", as.addonID)
		RemoveCallback(cid)
	case result := <-c:
		if err := json.Unmarshal(result, &torrents); err != nil {
			log.Errorf("Failed to unmarshal torrents: %s", err)
		}
	}

	return torrents
}

// SearchLinks ...
func (as *AddonSearcher) SearchLinks(query string) []*bittorrent.TorrentFile {
	return as.call("search", as.GetQuerySearchObject(query))
}

// SearchMovieLinks ...
func (as *AddonSearcher) SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile {
	if movie == nil {
		return []*bittorrent.TorrentFile{}
	}

	return as.call("search_movie", as.GetMovieSearchObject(movie))
}

// SearchMovieLinksSilent ...
func (as *AddonSearcher) SearchMovieLinksSilent(movie *tmdb.Movie, withAuth bool) []*bittorrent.TorrentFile {
	if movie == nil {
		return []*bittorrent.TorrentFile{}
	}

	return as.call("search_movie", as.GetMovieSearchSilentObject(movie, withAuth))
}

// SearchSeasonLinks ...
func (as *AddonSearcher) SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile {
	if show == nil || season == nil {
		return []*bittorrent.TorrentFile{}
	}

	return as.call("search_season", as.GetSeasonSearchObject(show, season))
}

// SearchEpisodeLinks ...
func (as *AddonSearcher) SearchEpisodeLinks(show *tmdb.Show, episode *tmdb.Episode) []*bittorrent.TorrentFile {
	if show == nil || episode == nil {
		return []*bittorrent.TorrentFile{}
	}

	return as.call("search_episode", as.GetEpisodeSearchObject(show, episode))
}
