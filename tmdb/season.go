package tmdb

import (
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/playcount"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
	"github.com/jmcvetta/napping"
)

// GetSeason ...
func GetSeason(showID int, seasonNumber int, language string, seasonsCount int) *Season {
	var season *Season
	cacheStore := cache.NewDBStore()
	updateFrequency := config.Get().UpdateFrequency * 60
	if updateFrequency == 0 {
		updateFrequency = 1440
	} else {
		updateFrequency = updateFrequency - 1
	}
	// Last season should not be savedfor too long
	if seasonNumber == seasonsCount {
		updateFrequency = 1440
	}

	key := fmt.Sprintf("com.tmdb.season.%d.%d.%s", showID, seasonNumber, language)
	if err := cacheStore.Get(key, &season); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/tv/%d/season/%d", tmdbEndpoint, showID, seasonNumber),
			Params: napping.Params{
				"api_key":            apiKey,
				"append_to_response": "credits,images,videos,external_ids,alternative_titles,translations,trailers",
				"language":           language,
			}.AsUrlValues(),
			Result:      &season,
			Description: "season",
		})

		if season == nil && err != nil && err == util.ErrNotFound {
			cacheStore.Set(key, &season, cacheHalfExpiration)
		}
		if season == nil {
			return nil
		}

		season.EpisodeCount = len(season.Episodes)

		// Fix for shows that have translations but return empty strings
		// for episode names and overviews.
		// We detect if episodes have their name filled, and if not re-query
		// with no language set.
		// See https://github.com/scakemyer/plugin.video.quasar/issues/249
		if season.EpisodeCount > 0 {
			for index := 0; index < season.EpisodeCount && index < len(season.Episodes); index++ {
				if season.Episodes[index] != nil && season.Episodes[index].Name == "" {
					season.Episodes[index] = GetEpisode(showID, seasonNumber, index+1, language)
				}
			}
		}

		cacheStore.Set(key, &season, time.Duration(updateFrequency)*time.Minute)
	}
	return season
}

// ToListItems ...
func (seasons SeasonList) ToListItems(show *Show) []*xbmc.ListItem {
	items := make([]*xbmc.ListItem, 0, len(seasons))
	specials := make(xbmc.ListItems, 0)

	fanarts := make([]string, 0)
	for _, backdrop := range show.Images.Backdrops {
		fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))
	}

	now := util.UTCBod()

	if config.Get().ShowSeasonsOrder == 0 {
		sort.Slice(seasons, func(i, j int) bool { return seasons[i].Season < seasons[j].Season })
	} else {
		sort.Slice(seasons, func(i, j int) bool { return seasons[i].Season > seasons[j].Season })
	}

	for _, season := range seasons {
		if season.EpisodeCount == 0 {
			continue
		}
		if config.Get().ShowUnairedSeasons == false {
			firstAired, _ := time.Parse("2006-01-02", season.AirDate)
			if firstAired.After(now) || firstAired.Equal(now) {
				continue
			}
		}

		item := season.ToListItem(show)

		if len(fanarts) > 0 {
			item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
		}

		if season.Season <= 0 {
			specials = append(specials, item)
		} else {
			items = append(items, item)
		}
	}

	return append(items, specials...)
}

func (seasons SeasonList) Len() int           { return len(seasons) }
func (seasons SeasonList) Swap(i, j int)      { seasons[i], seasons[j] = seasons[j], seasons[i] }
func (seasons SeasonList) Less(i, j int) bool { return seasons[i].Season < seasons[j].Season }

// ToListItem ...
func (season *Season) ToListItem(show *Show) *xbmc.ListItem {
	name := fmt.Sprintf("Season %d", season.Season)
	if season.Name != "" {
		name = season.Name
	}
	if season.Season == 0 {
		name = "Specials"
	}

	item := &xbmc.ListItem{
		Label: name,
		Info: &xbmc.ListItemInfo{
			Count:         rand.Int(),
			Title:         name,
			OriginalTitle: name,
			Season:        season.Season,
			TVShowTitle:   show.OriginalName,
			Plot:          show.Overview,
			PlotOutline:   show.Overview,
			DBTYPE:        "season",
			Mediatype:     "season",
			Code:          show.ExternalIDs.IMDBId,
			IMDBNumber:    show.ExternalIDs.IMDBId,
			PlayCount:     playcount.GetWatchedSeasonByTMDB(show.ID, season.Season).Int(),
		},
		Art: &xbmc.ListItemArt{},
	}

	if season.Poster != "" {
		item.Art.Poster = ImageURL(season.Poster, "w500")
		item.Art.Thumbnail = ImageURL(season.Poster, "w500")
	}

	fanarts := make([]string, 0)
	for _, backdrop := range show.Images.Backdrops {
		fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))
	}
	if len(fanarts) > 0 {
		item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
	}

	if config.Get().UseFanartTv {
		if fa := fanart.GetShow(util.StrInterfaceToInt(show.ExternalIDs.TVDBID)); fa != nil {
			item.Art = fa.ToSeasonListItemArt(season.Season, item.Art)
			item.Thumbnail = item.Art.Thumbnail
		}
	}

	if len(show.Genres) > 0 {
		item.Info.Genre = show.Genres[0].Name
	}

	return item
}
