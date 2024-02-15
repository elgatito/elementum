package tmdb

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/jmcvetta/napping"
)

// GetSeason ...
func GetSeason(showID int, seasonNumber int, language string, seasonsCount int, includeEpisodes bool) *Season {
	defer perf.ScopeTimer()()

	var season *Season
	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/tv/%d/season/%d", showID, seasonNumber),
		Params: napping.Params{
			"api_key":                apiKey,
			"append_to_response":     "credits,images,videos,external_ids,alternative_titles,translations,trailers",
			"include_image_language": fmt.Sprintf("%s,en,null", config.Get().Language),
			"include_video_language": fmt.Sprintf("%s,en,null", config.Get().Language),
			"language":               language,
		}.AsUrlValues(),
		Result:      &season,
		Description: "season",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()

	if season == nil {
		return nil
	}

	season.EpisodeCount = len(season.Episodes)

	// Fix for shows that have translations but return empty strings
	// for episode names and overviews.
	// We detect if episodes have their name filled, and if not re-query
	// with no language set.
	// See https://github.com/scakemyer/plugin.video.quasar/issues/249
	if season.EpisodeCount > 0 && includeEpisodes {
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

	return season
}

// ToListItems ...
func (seasons SeasonList) ToListItems(show *Show) []*xbmc.ListItem {
	defer perf.ScopeTimer()()

	items := make([]*xbmc.ListItem, 0, len(seasons))
	specials := make(xbmc.ListItems, 0)

	if config.Get().ShowSeasonsOrder == 0 {
		sort.Slice(seasons, func(i, j int) bool { return seasons[i].Season < seasons[j].Season })
	} else {
		sort.Slice(seasons, func(i, j int) bool { return seasons[i].Season > seasons[j].Season })
	}

	// If we have empty Names/Overviews then we need to collect Translations separately
	wg := sync.WaitGroup{}
	for i, season := range seasons {
		if season == nil {
			continue
		}

		if season.Translations == nil && (season.Name == "" || season.Overview == "" || len(season.Episodes) == 0) {
			wg.Add(1)
			go func(idx int, season *Season) {
				defer wg.Done()
				seasons[idx] = GetSeason(show.ID, season.Season, config.Get().Language, len(seasons), false)
			}(i, season)
		}
	}
	wg.Wait()

	for _, season := range seasons {
		if season == nil || season.EpisodeCount == 0 {
			continue
		}

		if !config.Get().ShowUnairedSeasons {
			if _, isExpired := util.AirDateWithExpireCheck(season.AirDate, time.DateOnly, config.Get().ShowEpisodesOnReleaseDay); isExpired {
				continue
			}
		}
		if !config.Get().ShowSeasonsSpecials && season.Season <= 0 {
			continue
		}

		item := season.ToListItem(show)

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

// SetArt sets artworks for season
func (season *Season) SetArt(show *Show, item *xbmc.ListItem) {
	if item.Art == nil {
		item.Art = &xbmc.ListItemArt{}
	}

	// Use the show's artwork as a fallback
	show.SetArt(item)

	if season.Poster != "" {
		item.Art.Poster = ImageURL(season.Poster, "w1280")
		item.Art.Thumbnail = ImageURL(season.Poster, "w1280")
	}
	if season.Backdrop != "" {
		item.Art.FanArt = ImageURL(season.Backdrop, "w1280")
		item.Art.Banner = ImageURL(season.Backdrop, "w1280")
	}

	if item.Art.AvailableArtworks == nil {
		item.Art.AvailableArtworks = &xbmc.Artworks{}
	}

	if season.Images != nil && season.Images.Backdrops != nil {
		fanarts := make([]string, 0)
		foundLanguageSpecificImage := false
		for _, backdrop := range season.Images.Backdrops {
			// for AvailableArtworks
			fanarts = append(fanarts, ImageURL(backdrop.FilePath, "w1280"))

			// try to use language specific art
			if !foundLanguageSpecificImage && backdrop.Iso639_1 == config.Get().Language {
				item.Art.FanArt = ImageURL(backdrop.FilePath, "w1280")
				item.Art.Banner = ImageURL(backdrop.FilePath, "w1280")
				foundLanguageSpecificImage = true // we take first image, it has top rating
			}
		}
		if len(fanarts) > 0 {
			item.Art.FanArts = fanarts
			item.Art.AvailableArtworks.FanArt = fanarts
			item.Art.AvailableArtworks.Banner = fanarts
		}
	}

	if season.Images != nil && season.Images.Posters != nil {
		posters := make([]string, 0)
		foundLanguageSpecificImage := false
		for _, poster := range season.Images.Posters {
			// for AvailableArtworks
			posters = append(posters, ImageURL(poster.FilePath, "w1280"))

			// try to use language specific art
			if !foundLanguageSpecificImage && poster.Iso639_1 == config.Get().Language {
				item.Art.Poster = ImageURL(poster.FilePath, "w1280")
				item.Art.Thumbnail = ImageURL(poster.FilePath, "w1280")
				foundLanguageSpecificImage = true // we take first image, it has top rating
			}
		}
		if len(posters) > 0 {
			item.Art.AvailableArtworks.Poster = posters
		}
	}

	if config.Get().UseFanartTv {
		if show.FanArt == nil && show.ExternalIDs != nil {
			show.FanArt = fanart.GetShow(util.StrInterfaceToInt(show.ExternalIDs.TVDBID))
		}
		if show.FanArt != nil {
			item.Art = show.FanArt.ToSeasonListItemArt(season.Season, item.Art)
		}
	}

	item.Thumbnail = item.Art.Thumbnail
}

// ToListItem ...
func (season *Season) ToListItem(show *Show) *xbmc.ListItem {
	defer perf.ScopeTimer()()

	name := fmt.Sprintf("Season %d", season.Season)
	if season.name(show) != "" {
		name = season.name(show)
	}
	if season.Season == 0 {
		name = "Specials"
	}

	if config.Get().ShowUnwatchedEpisodesNumber {
		season.EpisodeCount = season.CountEpisodesNumber(show)
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
			PlayCount:     playcount.GetWatchedSeasonByTMDB(show.ID, season.Season).Int(),
			Genre:         show.GetGenres(),
			Studio:        show.GetStudios(),
		},
		Properties: &xbmc.ListItemProperties{
			TotalEpisodes: strconv.Itoa(season.EpisodeCount),
			ShowTMDBId:    strconv.Itoa(show.ID),
		},
		UniqueIDs: &xbmc.UniqueIDs{
			TMDB: strconv.Itoa(season.ID),
		},
	}
	if show.ExternalIDs != nil {
		item.Info.Code = show.ExternalIDs.IMDBId
		item.Info.IMDBNumber = show.ExternalIDs.IMDBId
	}

	if ls, err := uid.GetShowByTMDB(show.ID); ls != nil && err == nil {
		if lse := ls.GetSeason(season.Season); lse != nil {
			item.Info.DBID = lse.UIDs.Kodi
		}
	}

	if config.Get().ShowUnwatchedEpisodesNumber && item.Properties != nil {
		watchedEpisodes := season.CountWatchedEpisodesNumber(show)
		item.Properties.WatchedEpisodes = strconv.Itoa(watchedEpisodes)
		item.Properties.UnWatchedEpisodes = strconv.Itoa(season.EpisodeCount - watchedEpisodes)
	}

	season.SetArt(show, item)

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
		if tr == nil {
			continue
		}

		if strings.ToLower(tr.Iso639_1) == language {
			return tr
		}
	}

	return nil
}

// CountWatchedEpisodesNumber returns number of watched episodes
func (season *Season) CountWatchedEpisodesNumber(show *Show) (watchedEpisodes int) {
	c := config.Get()
	if !c.ShowSeasonsSpecials && season.Season <= 0 {
		return
	}
	if show == nil {
		return
	}

	if playcount.GetWatchedSeasonByTMDB(show.ID, season.Season) {
		return season.EpisodeCount
	}

	if show.IsSeasonAired(season.Season) || c.ShowUnairedEpisodes {
		for i := 1; i <= season.EpisodeCount; i++ {
			if playcount.GetWatchedEpisodeByTMDB(show.ID, season.Season, i) {
				watchedEpisodes++
			}
		}
	} else if show.LastEpisodeToAir != nil && show.LastEpisodeToAir.SeasonNumber == season.Season {
		for i := 1; i <= show.LastEpisodeToAir.EpisodeNumber; i++ {
			if playcount.GetWatchedEpisodeByTMDB(show.ID, season.Season, i) {
				watchedEpisodes++
			}
		}
	} else {
		s := show.EnsureSeason(season.Season)
		if s == nil || s.Episodes == nil {
			return
		}

		for _, episode := range s.Episodes {
			if episode == nil {
				continue
			} else if !c.ShowUnairedEpisodes {
				if episode.AirDate == "" {
					continue
				}
				if _, isExpired := util.AirDateWithExpireCheck(episode.AirDate, time.DateOnly, c.ShowEpisodesOnReleaseDay); isExpired {
					continue
				}
			}

			if playcount.GetWatchedEpisodeByTMDB(show.ID, episode.SeasonNumber, episode.EpisodeNumber) {
				watchedEpisodes++
			}
		}
	}
	return
}

// CountEpisodesNumber returns number of episodes
func (season *Season) CountEpisodesNumber(show *Show) (episodes int) {
	c := config.Get()
	if !c.ShowSeasonsSpecials && season.Season <= 0 {
		return
	}
	if show == nil {
		return
	}

	if show.IsSeasonAired(season.Season) || c.ShowUnairedEpisodes {
		return season.EpisodeCount
	} else if show.LastEpisodeToAir != nil && show.LastEpisodeToAir.SeasonNumber == season.Season {
		return show.LastEpisodeToAir.EpisodeNumber
	} else {
		s := show.EnsureSeason(season.Season)
		if s == nil || s.Episodes == nil {
			return season.EpisodeCount
		}

		for _, episode := range s.Episodes {
			if episode == nil {
				continue
			} else if !c.ShowUnairedEpisodes {
				if episode.AirDate == "" {
					continue
				}
				if _, isExpired := util.AirDateWithExpireCheck(episode.AirDate, time.DateOnly, c.ShowEpisodesOnReleaseDay); isExpired {
					continue
				}
			}

			episodes++
		}
		return
	}
}

// HasEpisode checks if episode with specific number is available in the episodes list
func (season *Season) HasEpisode(episode int) bool {
	if len(season.Episodes) <= 0 {
		return false
	}

	for _, e := range season.Episodes {
		if e != nil && e.EpisodeNumber == episode {
			return true
		}
	}
	return false
}

// GetEpisode gets episode with specific number from Episodes list
func (season *Season) GetEpisode(episode int) *Episode {
	if len(season.Episodes) <= 0 {
		return nil
	}

	for _, e := range season.Episodes {
		if e != nil && e.EpisodeNumber == episode {
			return e
		}
	}
	return nil
}
