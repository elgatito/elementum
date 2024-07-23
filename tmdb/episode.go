package tmdb

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/fanart"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
	"github.com/elgatito/elementum/xbmc"
)

// GetEpisode ...
func GetEpisode(showID int, seasonNumber int, episodeNumber int, language string) *Episode {
	defer perf.ScopeTimer()()

	var episode *Episode
	languagesList := GetUserLanguages()

	req := reqapi.Request{
		API: reqapi.TMDBAPI,
		URL: fmt.Sprintf("/tv/%d/season/%d/episode/%d", showID, seasonNumber, episodeNumber),
		Params: napping.Params{
			"api_key":                apiKey,
			"append_to_response":     "credits,images,videos,translations,external_ids",
			"include_image_language": languagesList,
			"include_video_language": languagesList,
			"language":               language,
		}.AsUrlValues(),
		Result:      &episode,
		Description: "episode",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}

	req.Do()
	return episode
}

// ToListItems ...
func (episodes EpisodeList) ToListItems(show *Show, season *Season) []*xbmc.ListItem {
	defer perf.ScopeTimer()()

	items := make([]*xbmc.ListItem, 0, len(episodes))
	if len(episodes) == 0 {
		return items
	}

	for _, episode := range episodes {
		if episode == nil {
			continue
		}

		if !config.Get().ShowUnairedEpisodes {
			if episode.AirDate == "" {
				continue
			}
			if _, isAired := util.AirDateWithAiredCheck(episode.AirDate, time.DateOnly, config.Get().ShowEpisodesOnReleaseDay); !isAired {
				continue
			}
		}

		item := episode.ToListItem(show, season)

		items = append(items, item)
	}
	return items
}

// SetArt sets artworks for episode
func (episode *Episode) SetArt(show *Show, season *Season, item *xbmc.ListItem) {
	if item.Art == nil {
		item.Art = &xbmc.ListItemArt{}
	}

	// Episode only have Still aka Thumbnail, thus we take other artworks from the season/show
	if season != nil {
		season.SetArt(show, item)
	}

	imageQualities := GetImageQualities()

	if episode.StillPath != "" {
		item.Art.Thumbnail = ImageURL(episode.StillPath, imageQualities.Thumbnail)

		// Last resort: if show and season does not have Poster/FanArt - we use Still
		if item.Art.Poster == "" {
			item.Art.Poster = ImageURL(episode.StillPath, imageQualities.Thumbnail)
		}
		if item.Art.FanArt == "" {
			item.Art.FanArt = ImageURL(episode.StillPath, imageQualities.Thumbnail)
		}
	}

	if item.Art.AvailableArtworks == nil {
		item.Art.AvailableArtworks = &xbmc.Artworks{}
	}

	// This only will set available thumbnails
	SetLocalizedArt(&episode.Entity, item)

	if config.Get().UseFanartTv && show != nil {
		if show.FanArt == nil && show.ExternalIDs != nil {
			show.FanArt = fanart.GetShow(util.StrInterfaceToInt(show.ExternalIDs.TVDBID))
		}
		if show.FanArt != nil && season != nil {
			item.Art = show.FanArt.ToEpisodeListItemArt(season.Season, item.Art)
		}
	}

	item.Thumbnail = item.Art.Thumbnail
}

// ToListItem ...
func (episode *Episode) ToListItem(show *Show, season *Season) *xbmc.ListItem {
	defer perf.ScopeTimer()()

	year, _ := strconv.Atoi(strings.Split(episode.AirDate, "-")[0])

	episodeLabel := episode.GetName(show)
	if config.Get().AddEpisodeNumbers {
		episodeLabel = fmt.Sprintf("%dx%02d %s", episode.SeasonNumber, episode.EpisodeNumber, episode.GetName(show))
	}

	runtime := episode.Runtime * 60
	if runtime == 0 && len(show.EpisodeRunTime) > 0 {
		runtime = show.EpisodeRunTime[len(show.EpisodeRunTime)-1] * 60
	}

	item := &xbmc.ListItem{
		Label:  episodeLabel,
		Label2: fmt.Sprintf("%f", episode.VoteAverage),
		Info: &xbmc.ListItemInfo{
			Year:          year,
			Aired:         episode.AirDate,
			Count:         rand.Int(),
			Title:         episodeLabel,
			OriginalTitle: episode.GetName(show),
			Season:        episode.SeasonNumber,
			Episode:       episode.EpisodeNumber,
			TVShowTitle:   show.GetName(),
			Plot:          episode.overview(show),
			PlotOutline:   episode.overview(show),
			Rating:        episode.VoteAverage,
			Votes:         strconv.Itoa(episode.VoteCount),
			Duration:      runtime,
			PlayCount:     playcount.GetWatchedEpisodeByTMDB(show.ID, episode.SeasonNumber, episode.EpisodeNumber).Int(),
			MPAA:          show.mpaa(),
			DBTYPE:        "episode",
			Mediatype:     "episode",
			Genre:         show.GetGenres(),
			Studio:        show.GetStudios(),
			Country:       show.GetCountries(),
		},
		UniqueIDs: &xbmc.UniqueIDs{
			TMDB: strconv.Itoa(episode.ID),
		},
		Properties: &xbmc.ListItemProperties{
			ShowTMDBId: strconv.Itoa(show.ID),
		},
	}
	if show.ExternalIDs != nil {
		item.Info.Code = show.ExternalIDs.IMDBId
		item.Info.IMDBNumber = show.ExternalIDs.IMDBId
	}

	if ls, err := uid.GetShowByTMDB(show.ID); ls != nil && err == nil {
		if le := ls.GetEpisode(episode.SeasonNumber, episode.EpisodeNumber); le != nil {
			item.Info.DBID = le.UIDs.Kodi
			if le.Resume != nil {
				log.Debugf("%s S%02dE%02d le.Resume.Position: %f", show.OriginalName, episode.SeasonNumber, episode.EpisodeNumber, le.Resume.Position)
				item.Properties.ResumeTime = strconv.FormatFloat(le.Resume.Position, 'f', 6, 64)
				item.Properties.TotalTime = strconv.FormatFloat(le.Resume.Total, 'f', 6, 64)
			}
		}
	}

	episode.SetArt(show, season, item)

	SetTrailer(&episode.Entity, item)

	if season != nil && episode.Credits == nil && season.Credits != nil {
		episode.Credits = season.Credits
	}
	if episode.Credits == nil && show.Credits != nil {
		episode.Credits = show.Credits
	}
	if episode.Credits != nil {
		item.CastMembers = episode.Credits.GetCastMembers()
		item.Info.Director = episode.Credits.GetDirectors()
		item.Info.Writer = episode.Credits.GetWriters()
	}

	return item
}

func (episode *Episode) GetName(show *Show) string {
	if episode.Name != "" || episode.Translations == nil || episode.Translations.Translations == nil || len(episode.Translations.Translations) == 0 {
		return episode.Name
	}

	current := episode.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	current = episode.findTranslation(config.Get().SecondLanguage)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	if config.Get().Language != "en" && config.Get().SecondLanguage != "en" {
		current = episode.findTranslation("en")
		if current != nil && current.Data != nil && current.Data.Name != "" {
			return current.Data.Name
		}
	}

	current = episode.findTranslation(show.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Name != "" {
		return current.Data.Name
	}

	return episode.Name
}

func (episode *Episode) overview(show *Show) string {
	if episode.Overview != "" || episode.Translations == nil || episode.Translations.Translations == nil || len(episode.Translations.Translations) == 0 {
		return episode.Overview
	}

	current := episode.findTranslation(config.Get().Language)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	current = episode.findTranslation(config.Get().SecondLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	if config.Get().Language != "en" && config.Get().SecondLanguage != "en" {
		current = episode.findTranslation("en")
		if current != nil && current.Data != nil && current.Data.Overview != "" {
			return current.Data.Overview
		}
	}

	current = episode.findTranslation(show.OriginalLanguage)
	if current != nil && current.Data != nil && current.Data.Overview != "" {
		return current.Data.Overview
	}

	return episode.Overview
}

func (episode *Episode) findTranslation(language string) *Translation {
	if language == "" || episode.Translations == nil || episode.Translations.Translations == nil || len(episode.Translations.Translations) == 0 {
		return nil
	}

	language = strings.ToLower(language)
	for _, tr := range episode.Translations.Translations {
		if strings.ToLower(tr.Iso639_1) == language {
			return tr
		}
	}

	return nil
}

func (episode *Episode) GetLowestAirDate(airDate string, airDateFormat string) (newAirDate string, newAirDateFormat string) {
	newAirDate = episode.AirDate
	newAirDateFormat = time.DateOnly

	if config.Get().TraktUseLowestReleaseDate {
		airDateParsed, err1 := time.Parse(airDateFormat, airDate)
		tmdbAirDateParsed, err2 := time.Parse(time.DateOnly, episode.AirDate)
		if err1 == nil && err2 == nil && airDateParsed.Before(tmdbAirDateParsed) {
			newAirDate = airDate
			newAirDateFormat = airDateFormat
		}
	}

	return
}
