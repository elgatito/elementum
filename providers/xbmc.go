package providers

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/tmdb"
	"github.com/scakemyer/quasar/tvdb"
	"github.com/scakemyer/quasar/util"
	"github.com/scakemyer/quasar/xbmc"
)

const (
	// if >= 80% of episodes have absolute numbers, assume it's because we need it
	mixAbsoluteNumberPercentage = 0.8
)

type AddonSearcher struct {
	MovieSearcher
	SeasonSearcher
	EpisodeSearcher

	addonId string
	log     *logging.Logger
}

var cbLock = sync.RWMutex{}
var callbacks = map[string]chan []byte{}

func GetCallback() (string, chan []byte) {
	cid := strconv.Itoa(rand.Int())
	c := make(chan []byte, 1) // make sure we don't block clients when we write on it
	cbLock.Lock()
	callbacks[cid] = c
	cbLock.Unlock()
	return cid, c
}

func RemoveCallback(cid string) {
	cbLock.Lock()
	defer cbLock.Unlock()

	delete(callbacks, cid)
}

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
	body, _ := ioutil.ReadAll(ctx.Request.Body)
	c <- body
	close(c)
}

func getSearchers() []interface{} {
	list := make([]interface{}, 0)
	for _, addon := range xbmc.GetAddons("xbmc.python.script", "executable", true).Addons {
		if strings.HasPrefix(addon.ID, "script.quasar.") {
			list = append(list, NewAddonSearcher(addon.ID))
		}
	}
	return list
}

func GetMovieSearchers() []MovieSearcher {
	searchers := make([]MovieSearcher, 0)
	for _, searcher := range getSearchers() {
		searchers = append(searchers, searcher.(MovieSearcher))
	}
	return searchers
}

func GetSeasonSearchers() []SeasonSearcher {
	searchers := make([]SeasonSearcher, 0)
	for _, searcher := range getSearchers() {
		searchers = append(searchers, searcher.(SeasonSearcher))
	}
	return searchers
}

func GetEpisodeSearchers() []EpisodeSearcher {
	searchers := make([]EpisodeSearcher, 0)
	for _, searcher := range getSearchers() {
		searchers = append(searchers, searcher.(EpisodeSearcher))
	}
	return searchers
}

func GetSearchers() []Searcher {
	searchers := make([]Searcher, 0)
	for _, searcher := range getSearchers() {
		searchers = append(searchers, searcher.(Searcher))
	}
	return searchers
}

func NewAddonSearcher(addonId string) *AddonSearcher {
	return &AddonSearcher{
		addonId: addonId,
		log:     logging.MustGetLogger(fmt.Sprintf("AddonSearcher %s", addonId)),
	}
}

func (as *AddonSearcher) GetMovieSearchObject(movie *tmdb.Movie) *MovieSearchObject {
	year, _ := strconv.Atoi(strings.Split(movie.ReleaseDate, "-")[0])
	title := movie.OriginalTitle
	if title == "" {
		title = movie.Title
	}
	sObject := &MovieSearchObject{
		IMDBId: movie.IMDBId,
		Title:  NormalizeTitle(title),
		Year:   year,
		Titles: make(map[string]string),
	}
	for _, title := range movie.AlternativeTitles.Titles {
		sObject.Titles[strings.ToLower(title.ISO_3166_1)] = NormalizeTitle(title.Title)
	}
	return sObject
}

func (as *AddonSearcher) GetSeasonSearchObject(show *tmdb.Show, season *tmdb.Season) *SeasonSearchObject {
	year, _ := strconv.Atoi(strings.Split(season.AirDate, "-")[0])
	title := show.OriginalName
	if title == "" {
		title = show.Name
	}

	return &SeasonSearchObject{
		IMDBId:         show.ExternalIDs.IMDBId,
		TVDBId:         util.StrInterfaceToInt(show.ExternalIDs.TVDBID),
		Title:          NormalizeTitle(title),
		Year:           year,
		Season:         season.Season,
	}
}

func (as *AddonSearcher) GetEpisodeSearchObject(show *tmdb.Show, episode *tmdb.Episode) *EpisodeSearchObject {
	year, _ := strconv.Atoi(strings.Split(episode.AirDate, "-")[0])
	title := show.OriginalName
	if title == "" {
		title = show.Name
	}

	tvdbId := util.StrInterfaceToInt(show.ExternalIDs.TVDBID)

	// Is this an Anime?
	absoluteNumber := 0
	if util.StrInterfaceToInt(show.ExternalIDs.TVDBID) > 0 {
		countryIsJP := false
		for _, country := range show.OriginCountry {
			if country == "JP" {
				countryIsJP = true
				break
			}
		}
		genreIsAnim := false
		for _, genre := range show.Genres {
			if genre.Name == "Animation" {
				genreIsAnim = true
				break
			}
		}
		if countryIsJP && genreIsAnim {
			tvdbShow, err := tvdb.GetShow(tvdbId, config.Get().Language)
			if err == nil && len(tvdbShow.Seasons) >= episode.SeasonNumber + 1 {
				tvdbSeason := tvdbShow.Seasons[episode.SeasonNumber]
				if len(tvdbSeason.Episodes) >= episode.EpisodeNumber {
					tvdbEpisode := tvdbSeason.Episodes[episode.EpisodeNumber - 1]
					if tvdbEpisode.AbsoluteNumber > 0 {
						absoluteNumber = tvdbEpisode.AbsoluteNumber
					}
					title = tvdbShow.SeriesName
				}
			}
		}
	}

	return &EpisodeSearchObject{
		IMDBId:         show.ExternalIDs.IMDBId,
		TVDBId:         tvdbId,
		Title:          NormalizeTitle(title),
		Season:         episode.SeasonNumber,
		Episode:        episode.EpisodeNumber,
		Year:           year,
		AbsoluteNumber: absoluteNumber,
	}
}

func (as *AddonSearcher) call(method string, searchObject interface{}) []*bittorrent.TorrentFile {
	torrents := make([]*bittorrent.TorrentFile, 0)
	cid, c := GetCallback()
	cbUrl := fmt.Sprintf("%s/callbacks/%s", util.GetHTTPHost(), cid)

	payload := &SearchPayload{
		Method:       method,
		CallbackURL:  cbUrl,
		SearchObject: searchObject,
	}

	xbmc.ExecuteAddon(as.addonId, payload.String())

	timeout := providerTimeout()
	if config.Get().CustomProviderTimeoutEnabled == true {
		timeout = time.Duration(config.Get().CustomProviderTimeout) * time.Second
	}

	select {
	case <-time.After(timeout):
		as.log.Warningf("Provider %s was too slow. Ignored.", as.addonId)
		RemoveCallback(cid)
	case result := <-c:
		json.Unmarshal(result, &torrents)
	}

	return torrents
}

func (as *AddonSearcher) SearchLinks(query string) []*bittorrent.TorrentFile {
	return as.call("search", query)
}

func (as *AddonSearcher) SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile {
	return as.call("search_movie", as.GetMovieSearchObject(movie))
}

func (as *AddonSearcher) SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile {
	return as.call("search_season", as.GetSeasonSearchObject(show, season))
}

func (as *AddonSearcher) SearchEpisodeLinks(show *tmdb.Show, episode *tmdb.Episode) []*bittorrent.TorrentFile {
	return as.call("search_episode", as.GetEpisodeSearchObject(show, episode))
}
