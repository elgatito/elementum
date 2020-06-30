package tmdb

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/playcount"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
	"github.com/jmcvetta/napping"
)

// GetEpisode ...
func GetEpisode(showID int, seasonNumber int, episodeNumber int, language string) *Episode {
	var episode *Episode
	cacheStore := cache.NewDBStore()
	key := fmt.Sprintf("com.tmdb.episode.%d.%d.%d.%s", showID, seasonNumber, episodeNumber, language)
	if err := cacheStore.Get(key, &episode); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/tv/%d/season/%d/episode/%d", tmdbEndpoint, showID, seasonNumber, episodeNumber),
			Params: napping.Params{
				"api_key":            apiKey,
				"append_to_response": "credits,images,videos,alternative_titles,translations,external_ids,trailers",
				"language":           language,
			}.AsUrlValues(),
			Result:      &episode,
			Description: "episode",
		})

		if episode != nil {
			cacheStore.Set(key, episode, cacheExpiration)
		}
	}
	return episode
}

// ToListItems ...
func (episodes EpisodeList) ToListItems(show *Show, season *Season) []*xbmc.ListItem {
	items := make([]*xbmc.ListItem, 0, len(episodes))
	if len(episodes) == 0 {
		return items
	}

	fanarts := make([]string, 0)
	for _, backdrop := range show.Images.Backdrops {
		fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))
	}

	now := util.UTCBod()
	for _, episode := range episodes {
		if config.Get().ShowUnairedEpisodes == false {
			if episode.AirDate == "" {
				continue
			}
			firstAired, _ := time.Parse("2006-01-02", episode.AirDate)
			if firstAired.After(now) || firstAired.Equal(now) {
				continue
			}
		}

		item := episode.ToListItem(show, season)

		if item.Art.FanArt == "" && len(fanarts) > 0 {
			item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
		}

		if item.Art.FanArt == "" && season.Poster != "" {
			item.Art.Poster = ImageURL(season.Poster, "w500")
		}

		items = append(items, item)
	}
	return items
}

// ToListItem ...
func (episode *Episode) ToListItem(show *Show, season *Season) *xbmc.ListItem {
	episodeLabel := episode.Name
	if config.Get().AddEpisodeNumbers {
		episodeLabel = fmt.Sprintf("%dx%02d %s", episode.SeasonNumber, episode.EpisodeNumber, episode.Name)
	}

	runtime := 1800
	if len(show.EpisodeRunTime) > 0 {
		runtime = show.EpisodeRunTime[len(show.EpisodeRunTime)-1] * 60
	}

	item := &xbmc.ListItem{
		Label:  episodeLabel,
		Label2: fmt.Sprintf("%f", episode.VoteAverage),
		Info: &xbmc.ListItemInfo{
			Count:         rand.Int(),
			Title:         episodeLabel,
			OriginalTitle: episode.Name,
			Season:        episode.SeasonNumber,
			Episode:       episode.EpisodeNumber,
			TVShowTitle:   show.Name,
			Plot:          episode.Overview,
			PlotOutline:   episode.Overview,
			Rating:        episode.VoteAverage,
			Aired:         episode.AirDate,
			Duration:      runtime,
			Code:          show.ExternalIDs.IMDBId,
			IMDBNumber:    show.ExternalIDs.IMDBId,
			PlayCount:     playcount.GetWatchedEpisodeByTMDB(show.ID, episode.SeasonNumber, episode.EpisodeNumber).Int(),
			DBTYPE:        "episode",
			Mediatype:     "episode",
		},
		Art: &xbmc.ListItemArt{},
	}

	if show.PosterPath != "" {
		item.Art.TvShowPoster = ImageURL(show.PosterPath, "w500")
		item.Art.FanArt = ImageURL(show.BackdropPath, "w1280")
		item.Art.Thumbnail = ImageURL(show.PosterPath, "w500")
		item.Thumbnail = ImageURL(show.PosterPath, "w500")
	} else if show.Images != nil {
		fanarts := []string{}
		for _, backdrop := range show.Images.Backdrops {
			fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))
		}
		if len(fanarts) > 0 {
			item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
		}

		fanarts = []string{}
		for _, poster := range show.Images.Posters {
			fanarts = append(fanarts, ImageURL(poster.FilePath, "w500"))
		}
		if len(fanarts) > 0 {
			item.Art.TvShowPoster = fanarts[rand.Intn(len(fanarts))]
		}
	}

	if config.Get().UseFanartTv {
		if fa := fanart.GetShow(util.StrInterfaceToInt(show.ExternalIDs.TVDBID)); fa != nil {
			item.Art = fa.ToEpisodeListItemArt(season.Season, item.Art)
		}
	}

	if episode.StillPath != "" {
		item.Art.FanArt = ImageURL(episode.StillPath, "w1280")
		item.Art.Thumbnail = ImageURL(episode.StillPath, "w500")
		item.Art.Poster = ImageURL(episode.StillPath, "w500")
		item.Thumbnail = ImageURL(episode.StillPath, "w500")
	}

	genres := make([]string, 0, len(show.Genres))
	for _, genre := range show.Genres {
		genres = append(genres, genre.Name)
	}
	item.Info.Genre = strings.Join(genres, " / ")

	for _, company := range show.ProductionCompanies {
		item.Info.Studio = company.Name
		break
	}

	if season != nil && episode.Credits == nil && season.Credits != nil {
		episode.Credits = season.Credits
	}

	if episode.Credits != nil {
		item.Info.CastAndRole = make([][]string, 0)
		for _, cast := range episode.Credits.Cast {
			item.Info.CastAndRole = append(item.Info.CastAndRole, []string{cast.Name, cast.Character})
		}
		directors := make([]string, 0)
		writers := make([]string, 0)
		for _, crew := range episode.Credits.Crew {
			switch crew.Job {
			case "Director":
				directors = append(directors, crew.Name)
			case "Writer":
				writers = append(writers, crew.Name)
			}
		}
		item.Info.Director = strings.Join(directors, " / ")
		item.Info.Writer = strings.Join(writers, " / ")
	}

	return item
}
