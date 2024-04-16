package api

import (
	"fmt"
	"strconv"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
)

type providerDebugResponse struct {
	Payload interface{} `json:"payload"`
	Results interface{} `json:"results"`
}

// ProviderGetMovie ...
func ProviderGetMovie(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	tmdbID := ctx.Params.ByName("tmdbId")
	provider := ctx.Params.ByName("provider")
	log.Infof("Searching links for:", tmdbID)
	movie := tmdb.GetMovieByID(tmdbID, config.Get().Language)
	if movie == nil {
		ctx.Error(fmt.Errorf("Unable to get movie %s", tmdbID))
		return
	}
	log.Infof("Resolved %s to %s", tmdbID, movie.GetTitle())

	searcher := providers.NewAddonSearcher(xbmcHost, ctx.Request.Host, provider)
	torrents := searcher.SearchMovieLinks(movie)
	if ctx.Query("resolve") == "true" {
		for _, torrent := range torrents {
			torrent.Resolve()
		}
	}
	data, err := json.MarshalIndent(providerDebugResponse{
		Payload: searcher.GetMovieSearchObject(movie),
		Results: torrents,
	}, "", "    ")
	if err != nil {
		xbmcHost.AddonFailure(provider)
		ctx.Error(err)
	}
	ctx.Data(200, "application/json", data)
}

// ProviderGetEpisode ...
func ProviderGetEpisode(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	provider := ctx.Params.ByName("provider")
	showID, _ := strconv.Atoi(ctx.Params.ByName("showId"))
	seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
	episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))

	log.Infof("Searching links for TMDB Id:", showID)

	show := tmdb.GetShow(showID, config.Get().Language)
	if show == nil {
		ctx.Error(fmt.Errorf("Unable to get show %d", showID))
		return
	}

	season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language, len(show.Seasons), true)
	if season == nil {
		ctx.Error(fmt.Errorf("Unable to get season %d", seasonNumber))
		return
	} else if !season.HasEpisode(episodeNumber) {
		ctx.Error(fmt.Errorf("Unable to get episode %d", episodeNumber))
		return
	}

	episode := season.GetEpisode(episodeNumber)

	log.Infof("Resolved %d to %s", showID, show.GetName())

	searcher := providers.NewAddonSearcher(xbmcHost, ctx.Request.Host, provider)
	torrents := searcher.SearchEpisodeLinks(show, episode)
	if ctx.Query("resolve") == "true" {
		for _, torrent := range torrents {
			torrent.Resolve()
		}
	}
	data, err := json.MarshalIndent(providerDebugResponse{
		Payload: searcher.GetEpisodeSearchObject(show, episode),
		Results: torrents,
	}, "", "    ")
	if err != nil {
		xbmcHost.AddonFailure(provider)
		ctx.Error(err)
	}
	ctx.Data(200, "application/json", data)
}
