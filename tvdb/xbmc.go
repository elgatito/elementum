package tvdb

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

func imageURL(path string) string {
	return tvdbURL + "/banners/" + path
}

// ToListItems ...
func (seasons SeasonList) ToListItems(show *Show) []*xbmc.ListItem {
	items := make([]*xbmc.ListItem, 0, len(seasons))

	fanarts := make([]string, 0)
	for _, banner := range show.Banners {
		if banner.BannerType == "fanart" {
			fanarts = append(fanarts, imageURL(banner.BannerPath))
		}
	}

	now := util.UTCBod()
	for _, season := range seasons {
		if len(season.Episodes) == 0 {
			continue
		}
		airedDateTime := fmt.Sprintf("%s %s EST", season.Episodes[0].FirstAired, show.AirsTime)
		firstAired, _ := time.Parse("2006-01-02 3:04 PM MST", airedDateTime)
		if firstAired.Add(time.Duration(show.Runtime) * time.Minute).After(now) {
			continue
		}
		item := season.ToListItem(show)
		if len(fanarts) > 0 {
			item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
		}
		items = append(items, item)
	}
	return items
}

// ToListItems ...
func (episodes EpisodeList) ToListItems(show *Show) []*xbmc.ListItem {
	items := make([]*xbmc.ListItem, 0, len(episodes))
	if len(episodes) == 0 {
		return items
	}

	fanarts := make([]string, 0)
	for _, banner := range show.Banners {
		if banner.BannerType == "fanart" {
			fanarts = append(fanarts, imageURL(banner.BannerPath))
		}
	}

	now := util.UTCBod()
	for _, episode := range episodes {
		if episode.FirstAired == "" {
			continue
		}
		airedDateTime := fmt.Sprintf("%s %s EST", episode.FirstAired, show.AirsTime)
		firstAired, _ := time.Parse("2006-01-02 3:04 PM MST", airedDateTime)
		if firstAired.Add(time.Duration(show.Runtime) * time.Minute).After(now) {
			continue
		}
		item := episode.ToListItem(show)
		if len(fanarts) > 0 {
			item.Art.FanArt = fanarts[rand.Intn(len(fanarts))]
		}
		items = append(items, item)
	}
	return items
}

// ToListItem ...
func (season *Season) ToListItem(show *Show) *xbmc.ListItem {
	name := fmt.Sprintf("Season %d", season.Season)
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
			TVShowTitle:   show.SeriesName,
		},
		Art: &xbmc.ListItemArt{},
	}

	for _, banner := range show.Banners {
		if banner.BannerType2 == "season" &&
			banner.Season == season.Season &&
			banner.Language == show.Language &&
			item.Art.Poster == "" {
			item.Art.Poster = imageURL(banner.BannerPath)
			item.Art.Thumbnail = item.Art.Poster
			item.Thumbnail = item.Art.Poster
			break
		}
	}

	item.Info.Genre = strings.Replace(strings.Trim(show.Genre, "|"), "|", " / ", -1)

	return item
}

// ToListItem ...
func (episode *Episode) ToListItem(show *Show) *xbmc.ListItem {
	episodeLabel := episode.EpisodeName
	if config.Get().AddEpisodeNumbers {
		episodeLabel = fmt.Sprintf("%dx%02d %s", episode.SeasonNumber, episode.EpisodeNumber, episode.EpisodeName)
	}

	item := &xbmc.ListItem{
		Label: episodeLabel,
		Info: &xbmc.ListItemInfo{
			Count:         rand.Int(),
			Title:         episodeLabel,
			OriginalTitle: episode.EpisodeName,
			Season:        episode.SeasonNumber,
			Episode:       episode.EpisodeNumber,
			TVShowTitle:   show.SeriesName,
			Plot:          episode.Overview,
			PlotOutline:   episode.Overview,
			Aired:         episode.FirstAired,
		},
		Art: &xbmc.ListItemArt{
			Thumbnail: imageURL(episode.FileName),
			Poster:    imageURL(show.Poster),
		},
	}

	return item
}
