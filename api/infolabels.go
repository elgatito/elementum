package api

import (
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/sanity-io/litter"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

var (
	infoLabels = []string{
		"ListItem.DBID",
		"ListItem.DBTYPE",
		"ListItem.Mediatype",
		"ListItem.TMDB",
		"ListItem.UniqueId",

		"ListItem.Code",
		"ListItem.Country",
		"ListItem.Director",
		"ListItem.Duration",
		"ListItem.Episode",
		"ListItem.EpisodeName",
		"ListItem.Genre",
		"ListItem.IMDBNumber",
		"ListItem.Label",
		"ListItem.Label2",
		"ListItem.Mediatype",
		"ListItem.MPAA",
		"ListItem.OriginalTitle",
		"ListItem.Path",
		"ListItem.PlayCount",
		"ListItem.Plot",
		"ListItem.PlotOutline",
		"ListItem.Premiered",
		"ListItem.Rating",
		"ListItem.Season",
		"ListItem.Studio",
		"ListItem.Tagline",
		"ListItem.Thumb",
		"ListItem.Title",
		"ListItem.Trailer",
		"ListItem.TVShowTitle",
		"ListItem.Votes",
		"ListItem.Writer",
		"ListItem.Year",

		"ListItem.Art(thumb)",
		"ListItem.Art(poster)",
		"ListItem.Art(tvshowposter)",
		"ListItem.Art(banner)",
		"ListItem.Art(fanart)",
		"ListItem.Art(fanarts)",
		"ListItem.Art(clearart)",
		"ListItem.Art(clearlogo)",
		"ListItem.Art(landscape)",
		"ListItem.Art(icon)",
	}
)

func itemWithDefault(item, def string) string {
	if item == "" {
		return def
	}
	return item
}

func itemToList(item string) []string {
	return strings.Split(item, " / ")
}

func itemToInt(item string) int {
	ret, _ := strconv.Atoi(item)
	return ret
}

func itemToFloat(item string) float32 {
	ret, _ := strconv.ParseFloat(item, 32)
	return float32(ret)
}

func saveEncoded(xbmcHost *xbmc.XBMCHost, encoded string) {
	xbmcHost.SetWindowProperty("ListItem.Encoded", encoded)
}

func encodeItem(item *xbmc.ListItem) string {
	data, _ := json.Marshal(item)
	return string(data)
}

func labelsToListItem(labels map[string]string) *xbmc.ListItem {
	// Remove 'ListItem.' from labels map keys + lowercase all keys
	for k, v := range labels {
		key := strings.Replace(k, "ListItem.", "", 1)
		labels[strings.ToLower(key)] = v
	}

	return &xbmc.ListItem{
		Label:     labels["label"],
		Label2:    labels["label2"],
		Icon:      labels["icon"],
		Thumbnail: labels["thumb"],
		Path:      labels["path"],

		Info: &xbmc.ListItemInfo{
			Date: labels["premiered"],

			DBID:      itemToInt(labels["dbid"]),
			DBTYPE:    itemWithDefault(labels["dbtype"], "movie"),
			Mediatype: itemWithDefault(labels["dbtype"], "movie"),

			Genre:         itemToList(labels["genre"]),
			Country:       itemToList(labels["country"]),
			Year:          itemToInt(labels["year"]),
			Episode:       itemToInt(labels["episode"]),
			Season:        itemToInt(labels["season"]),
			Rating:        itemToFloat(labels["rating"]),
			PlayCount:     itemToInt(labels["playcount"]),
			Director:      itemToList(labels["director"]),
			MPAA:          labels["mpaa"],
			Plot:          labels["plot"],
			PlotOutline:   labels["plotoutline"],
			Title:         labels["title"],
			OriginalTitle: labels["originaltitle"],
			Duration:      itemToInt(labels["duration"]),
			Studio:        itemToList(labels["studio"]),
			TVShowTitle:   labels["tvshowtitle"],
			Premiered:     labels["premiered"],
			Aired:         labels["premiered"],
		},
		Properties: &xbmc.ListItemProperties{},
		Art: &xbmc.ListItemArt{
			Thumbnail:    labels["art(thumb)"],
			Poster:       labels["art(poster)"],
			TvShowPoster: labels["art(tvshowposter)"],
			Banner:       labels["art(banner)"],
			FanArt:       labels["art(fanart)"],
			ClearArt:     labels["art(clearart)"],
			ClearLogo:    labels["art(clearlogo)"],
			Landscape:    labels["art(landscape)"],
			Icon:         labels["art(icon)"],
		},
	}
}

// InfoLabelsStored ...
func InfoLabelsStored(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		labelsString := "{}"

		if listLabel := xbmcHost.InfoLabel("ListItem.Label"); len(listLabel) > 0 {
			item := labelsToListItem(xbmcHost.InfoLabels(infoLabels...))
			labelsString = encodeItem(item)
			saveEncoded(xbmcHost, labelsString)
		} else if encoded := xbmcHost.GetWindowProperty("ListItem.Encoded"); len(encoded) > 0 {
			labelsString = encoded
		}

		ctx.String(200, labelsString)
	}
}

// InfoLabelsEpisode ...
func InfoLabelsEpisode(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		tmdbID := ctx.Params.ByName("showId")
		showID, _ := strconv.Atoi(tmdbID)
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))

		if item, err := GetEpisodeLabels(showID, seasonNumber, episodeNumber); err == nil {
			saveEncoded(xbmcHost, encodeItem(item))
			ctx.JSON(200, item)
		} else {
			ctx.Error(err)
		}
	}
}

// InfoLabelsMovie ...
func InfoLabelsMovie(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		tmdbID := ctx.Params.ByName("tmdbId")

		if item, err := GetMovieLabels(tmdbID); err == nil {
			saveEncoded(xbmcHost, encodeItem(item))
			ctx.JSON(200, item)
		} else {
			ctx.Error(err)
		}
	}
}

// InfoLabelsSearch ...
func InfoLabelsSearch(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		tmdbID := ctx.Params.ByName("tmdbId")
		idx := ctx.DefaultQuery("index", "-1")

		if item, err := GetSearchLabels(s, tmdbID, idx); err == nil {
			saveEncoded(xbmcHost, encodeItem(item))
			ctx.JSON(200, item)
		} else {
			ctx.Error(err)
		}
	}
}

// GetEpisodeLabels returns ListItem for an episode
func GetEpisodeLabels(showID, seasonNumber, episodeNumber int) (item *xbmc.ListItem, err error) {
	show := tmdb.GetShow(showID, config.Get().Language)
	if show == nil {
		return nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language, len(show.Seasons), true)
	if season == nil {
		return nil, errors.New("Unable to find season")
	}

	episode := tmdb.GetEpisode(showID, seasonNumber, episodeNumber, config.Get().Language)
	if episode == nil {
		return nil, errors.New("Unable to find episode")
	}

	item = episode.ToListItem(show, season)
	if ls, err := uid.GetShowByTMDB(show.ID); ls != nil && err == nil {
		log.Debugf("Found show in library: %s", litter.Sdump(ls.UIDs))
		if le := ls.GetEpisode(episode.SeasonNumber, episodeNumber); le != nil && item.Info != nil {
			item.Info.DBID = le.UIDs.Kodi
		}
	}

	return
}

// GetMovieLabels returns ListItem for a movie
func GetMovieLabels(tmdbID string) (item *xbmc.ListItem, err error) {
	movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)
	if movie == nil {
		return nil, errors.New("Unable to find movie")
	}

	item = movie.ToListItem()
	if lm, err := uid.GetMovieByTMDB(movie.ID); lm != nil && err == nil && item.Info != nil {
		log.Debugf("Found movie in library: %s", litter.Sdump(lm))
		item.Info.DBID = lm.UIDs.Kodi
	}

	return
}

// GetSearchLabels returns ListItem for a search query
func GetSearchLabels(s *bittorrent.Service, tmdbID string, idx string) (item *xbmc.ListItem, err error) {
	torrent := s.HasTorrentByFakeID(tmdbID)
	if torrent == nil || torrent.DBItem == nil {
		return nil, errors.New("Unable to find the torrent")
	}

	// Collecting downloaded file names into string to show in a subtitle
	chosenFiles := map[string]bool{}
	chosenFileNames := []string{}

	if idxNum, errNum := strconv.Atoi(idx); errNum == nil && idxNum >= 0 {
		if f := torrent.GetCandidateFileForIndex(idxNum); f != nil {
			chosenFiles[filepath.Base(f.Path)] = true
		}
	}

	if len(chosenFiles) == 0 {
		for _, f := range torrent.ChosenFiles {
			chosenFiles[filepath.Base(f.Path)] = true
		}
	}

	for k := range chosenFiles {
		chosenFileNames = append(chosenFileNames, k)
	}

	sort.Strings(chosenFileNames)
	subtitle := strings.Join(chosenFileNames, ", ")

	item = &xbmc.ListItem{
		Label:  torrent.DBItem.Query,
		Label2: subtitle,
		Info: &xbmc.ListItemInfo{
			Title:         torrent.DBItem.Query,
			OriginalTitle: torrent.DBItem.Query,
			TVShowTitle:   subtitle,
			DBTYPE:        "episode",
			Mediatype:     "episode",
		},
		Art: &xbmc.ListItemArt{},
	}

	return
}
