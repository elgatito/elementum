package tmdb

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
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
	// Last season should not be saved for too long
	if seasonNumber == seasonsCount {
		updateFrequency = 1440
	}

	key := fmt.Sprintf(cache.TMDBSeasonKey, showID, seasonNumber, language)
	if err := cacheStore.Get(key, &season); err != nil {
		err = MakeRequest(APIRequest{
			URL: fmt.Sprintf("%s/tv/%d/season/%d", tmdbEndpoint, showID, seasonNumber),
			Params: napping.Params{
				"api_key":                apiKey,
				"append_to_response":     "credits,images,videos,external_ids,alternative_titles,translations,trailers",
				"include_image_language": fmt.Sprintf("%s,en,null", config.Get().Language),
				"language":               language,
			}.AsUrlValues(),
			Result:      &season,
			Description: "season",
		})

		if season == nil && err != nil && err == util.ErrNotFound {
			cacheStore.Set(key, &season, cache.TMDBSeasonExpire)
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
			// If we have empty Names/Overviews then we need to collect Translations separately
			wg := sync.WaitGroup{}
			for i, episode := range season.Episodes {
				if episode.Translations == nil && (episode.Name == "" || episode.Overview == "") {
					wg.Add(1)
					go func(idx int, episode *Episode) {
						defer wg.Done()
						season.Episodes[idx] = GetEpisode(showID, seasonNumber, idx+1, language)
					}(i, episode)
				}
			}
			wg.Wait()
		}

		cacheStore.Set(key, &season, cache.TMDBSeasonExpire)
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

	if config.Get().ShowSeasonsOrder == 0 {
		sort.Slice(seasons, func(i, j int) bool { return seasons[i].Season < seasons[j].Season })
	} else {
		sort.Slice(seasons, func(i, j int) bool { return seasons[i].Season > seasons[j].Season })
	}

	// If we have empty Names/Overviews then we need to collect Translations separately
	wg := sync.WaitGroup{}
	for i, season := range seasons {
		if season.Translations == nil && (season.Name == "" || season.Overview == "") {
			wg.Add(1)
			go func(idx int, season *Season) {
				defer wg.Done()
				seasons[idx] = GetSeason(show.ID, season.Season, config.Get().Language, len(seasons))
			}(i, season)
		}
	}
	wg.Wait()

	for _, season := range seasons {
		if season == nil || season.EpisodeCount == 0 {
			continue
		}

		if config.Get().ShowUnairedSeasons == false {
			if _, isExpired := util.AirDateWithExpireCheck(season.AirDate, config.Get().ShowEpisodesOnReleaseDay); isExpired {
				continue
			}
		}
		if !config.Get().ShowSeasonsSpecials && season.Season <= 0 {
			continue
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
	if season.name(show) != "" {
		name = season.name(show)
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
			TVShowTitle:   show.name(),
			Plot:          season.overview(show),
			PlotOutline:   season.overview(show),
			MPAA:          show.mpaa(),
			DBTYPE:        "season",
			Mediatype:     "season",
			Code:          show.ExternalIDs.IMDBId,
			IMDBNumber:    show.ExternalIDs.IMDBId,
			PlayCount:     playcount.GetWatchedSeasonByTMDB(show.ID, season.Season).Int(),
			Genre:         show.GetGenres(),
			Studio:        show.GetStudios(),
		},
		Properties: &xbmc.ListItemProperties{
			TotalEpisodes: strconv.Itoa(season.EpisodeCount),
		},
		Art: &xbmc.ListItemArt{
			TvShowPoster: ImageURL(show.PosterPath, "w1280"),
			FanArt:       ImageURL(season.Backdrop, "w1280"),
			Poster:       ImageURL(season.Poster, "w1280"),
			Thumbnail:    ImageURL(season.Poster, "w1280"),
		},
		UniqueIDs: &xbmc.UniqueIDs{
			TMDB: strconv.Itoa(season.ID),
		},
	}

	if ls, err := uid.GetShowByTMDB(show.ID); ls != nil && err == nil {
		if lse := ls.GetSeason(season.Season); lse != nil {
			item.Info.DBID = lse.UIDs.Kodi
		}
	}

	if config.Get().ShowUnwatchedEpisodesNumber {
		watchedEpisodes := season.watchedEpisodesNumber(show)
		item.Properties.WatchedEpisodes = strconv.Itoa(watchedEpisodes)
		item.Properties.UnWatchedEpisodes = strconv.Itoa(season.EpisodeCount - watchedEpisodes)
	}

	if item.Art.Poster == "" {
		item.Art.Poster = ImageURL(show.PosterPath, "w1280")
		item.Art.Thumbnail = ImageURL(show.PosterPath, "w1280")
	}

	var thisBackdrops []*Image
	if show.Images != nil && show.Images.Backdrops != nil && len(show.Images.Backdrops) != 0 {
		thisBackdrops = show.Images.Backdrops
	}
	if season.Images != nil && season.Images.Backdrops != nil && len(season.Images.Backdrops) != 0 {
		thisBackdrops = season.Images.Backdrops
	}
	fanarts := make([]string, 0)
	for _, backdrop := range thisBackdrops {
		fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))
	}
	if len(fanarts) > 0 {
		item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
		item.Art.FanArts = fanarts
	}

	if config.Get().UseFanartTv && show.FanArt != nil {
		item.Art = show.FanArt.ToSeasonListItemArt(season.Season, item.Art)
	}

	item.Thumbnail = item.Art.Thumbnail

	return item
}

func (season *Season) name(show *Show) string {
	if season.Name != "" || season.Translations == nil || season.Translations.Translations == nil || len(season.Translations.Translations) == 0 {
		return season.Name
	}

	current := season.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	current = season.findTranslation("en")
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	current = season.findTranslation(show.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	return season.Name
}

func (season *Season) overview(show *Show) string {
	if season.Overview != "" || season.Translations == nil || season.Translations.Translations == nil || len(season.Translations.Translations) == 0 {
		return season.Overview
	}

	current := season.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	current = season.findTranslation("en")
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	current = season.findTranslation(show.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	return season.Overview
}

func (season *Season) findTranslation(language string) *Translation {
	if language == "" || season.Translations == nil || season.Translations.Translations == nil || len(season.Translations.Translations) == 0 {
		return nil
	}

	language = strings.ToLower(language)
	for _, tr := range season.Translations.Translations {
		if strings.ToLower(tr.Iso639_1) == language {
			return tr
		}
	}

	return nil
}

// watchedEpisodesNumber returns number of watched episodes
func (season *Season) watchedEpisodesNumber(show *Show) int {
	watchedEpisodes := 0
	if playcount.GetWatchedSeasonByTMDB(show.ID, season.Season) {
		watchedEpisodes += season.EpisodeCount
	} else {
		for _, episode := range season.Episodes {
			if playcount.GetWatchedEpisodeByTMDB(show.ID, season.Season, episode.EpisodeNumber) {
				watchedEpisodes++
			}
		}
	}
	return watchedEpisodes
}
