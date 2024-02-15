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

		"ListItem.Label",
		"ListItem.Label2",
		"ListItem.ThumbnailImage",
		"ListItem.Title",
		"ListItem.OriginalTitle",
		"ListItem.TVShowTitle",
		"ListItem.Season",
		"ListItem.Episode",
		"ListItem.Premiered",
		"ListItem.Plot",
		"ListItem.PlotOutline",
		"ListItem.Tagline",
		"ListItem.Year",
		"ListItem.Trailer",
		"ListItem.Studio",
		"ListItem.MPAA",
		"ListItem.Genre",
		"ListItem.Mediatype",
		"ListItem.Writer",
		"ListItem.Director",
		"ListItem.Rating",
		"ListItem.Votes",
		"ListItem.IMDBNumber",
		"ListItem.Code",
		"ListItem.ArtFanart",
		"ListItem.ArtBanner",
		"ListItem.ArtPoster",
		"ListItem.ArtLandscape",
		"ListItem.ArtTvshowPoster",
		"ListItem.ArtClearArt",
		"ListItem.ArtClearLogo",
	}
)

func saveEncoded(xbmcHost *xbmc.XBMCHost, encoded string) {
	xbmcHost.SetWindowProperty("ListItem.Encoded", encoded)
}

func encodeItem(item *xbmc.ListItem) string {
	data, _ := json.Marshal(item)

	return string(data)
}

// InfoLabelsStored ...
func InfoLabelsStored(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)

		labelsString := "{}"

		if listLabel := xbmcHost.InfoLabel("ListItem.Label"); len(listLabel) > 0 {
			labels := xbmcHost.InfoLabels(infoLabels...)

			listItemLabels := make(map[string]string, len(labels))
			for k, v := range labels {
				key := strings.Replace(k, "ListItem.", "", 1)
				listItemLabels[key] = v
			}

			b, _ := json.Marshal(listItemLabels)
			labelsString = string(b)
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
