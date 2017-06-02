package api

import (
	"errors"
	"strings"
	"strconv"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/tmdb"
	"github.com/scakemyer/quasar/xbmc"
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
		"ListItem.ArtTvshowPoster",
	}
)

func saveEncoded(encoded string) {
	xbmc.SetWindowProperty("ListItem.Encoded", encoded)
}

func encodeItem(item *xbmc.ListItem) string {
	data, _ := json.Marshal(item)

	return string(data)
}

func InfoLabelsStored(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		labelsString := "{}"

		if listLabel := xbmc.InfoLabel("ListItem.Label"); len(listLabel) > 0 {
			labels := xbmc.InfoLabels(infoLabels...)

			listItemLabels := make(map[string]string, len(labels))
			for k, v := range labels {
				key := strings.Replace(k, "ListItem.", "", 1)
				listItemLabels[key] = v
			}

			b, _ := json.Marshal(listItemLabels)
			labelsString = string(b)
			saveEncoded(labelsString)
		} else if encoded := xbmc.GetWindowProperty("ListItem.Encoded"); len(encoded) > 0 {
			labelsString = encoded
		}

		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		ctx.String(200, labelsString)
	}
}

func InfoLabelsEpisode(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tmdbId := ctx.Params.ByName("showId")
		showId, _ := strconv.Atoi(tmdbId)
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))

		show := tmdb.GetShow(showId, config.Get().Language)
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		episode := tmdb.GetEpisode(showId, seasonNumber, episodeNumber, config.Get().Language)
		if episode == nil {
			ctx.Error(errors.New("Unable to find episode"))
			return
		}

		item := episode.ToListItem(show)
		if libraryItem := FindEpisodeInLibrary(show, episode); libraryItem != nil {
			item.Info.DBID = libraryItem.ID
		}

		saveEncoded(encodeItem(item))

		ctx.JSON(200, item)
	}
}

func InfoLabelsMovie(btService *bittorrent.BTService) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		tmdbId := ctx.Params.ByName("tmdbId")

		movie := tmdb.GetMovieById(tmdbId, config.Get().Language)
		if movie == nil {
			ctx.Error(errors.New("Unable to find movie"))
			return
		}

		item := movie.ToListItem()
		if libraryItem := FindMovieInLibrary(movie); libraryItem != nil {
			item.Info.DBID = libraryItem.ID
		}

		saveEncoded(encodeItem(item))

		ctx.JSON(200, item)
	}
}
