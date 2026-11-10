package library

import (
	"errors"
	"fmt"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/playcount"
	"github.com/elgatito/elementum/library/uid"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

var ErrWatchedMetadata = errors.New("could not load watched descendants")

// SetWatched updates the tracker and then the local hierarchy. Resolve descendants first so a
// metadata or tracker failure leaves the local state untouched. Kodi RPCs belong to the caller.
func SetWatched(item tracker.Item, watched bool) error {
	var seasons tmdb.SeasonList
	switch item.Type {
	case tracker.MovieType, tracker.EpisodeType:
	case tracker.ShowType:
		show := tmdb.GetShow(item.IDs.TMDB, config.Get().Language)
		if show == nil || show.ID == 0 {
			return ErrWatchedMetadata
		}
		seasons = append(tmdb.SeasonList(nil), show.Seasons...)
	case tracker.SeasonType:
		seasons = tmdb.SeasonList{{Season: item.Season}}
	default:
		return fmt.Errorf("unsupported watched type: %v", item.Type)
	}
	for i, season := range seasons {
		if season == nil {
			continue
		}
		s := tmdb.GetSeason(item.IDs.TMDB, season.Season, config.Get().Language, 0, false)
		if s == nil || s.ID == 0 {
			return ErrWatchedMetadata
		}
		seasons[i] = s
	}
	if tracker.Active() != nil {
		if err := tracker.SetWatched([]*tracker.WatchedItem{{Item: item}}, watched); err != nil {
			return err
		}
	}
	id := item.IDs.TMDB
	regular, complete := 0, 0
	switch item.Type {
	case tracker.MovieType:
		playcount.Set(playcount.MovieType, watched, id)
		return nil
	case tracker.EpisodeType:
		playcount.Set(playcount.EpisodeType, watched, id, item.Season, item.Episode)
		if !watched {
			playcount.Set(playcount.SeasonType, false, id, item.Season)
		}
	default:
		for _, season := range seasons {
			if season == nil {
				continue
			}
			aired := 0
			for _, episode := range season.Episodes {
				if episode == nil {
					continue
				}
				_, isAired := util.AirDateWithAiredCheck(episode.AirDate, time.DateOnly, config.Get().ShowEpisodesOnReleaseDay)
				if isAired {
					aired++
				}
				playcount.Set(playcount.EpisodeType, watched && isAired, id, season.Season, episode.EpisodeNumber)
			}
			// A bulk mark covers aired episodes; future or undated episodes keep the parent incomplete.
			seasonWatched := watched && aired > 0 && aired == len(season.Episodes)
			playcount.Set(playcount.SeasonType, seasonWatched, id, season.Season)
			if season.Season > 0 && len(season.Episodes) > 0 {
				regular++
				if seasonWatched {
					complete++
				}
			}
		}
	}
	// Keep a known show state even without Kodi or a populated tracker sync container.
	showWatched := bool(playcount.GetWatchedShowByTMDB(id))
	if item.Type == tracker.ShowType {
		showWatched = watched && regular > 0 && regular == complete
	} else if !watched {
		showWatched = false
	}
	playcount.Set(playcount.ShowType, showWatched, id)
	return nil
}

// KodiWatchedSetter returns nil when the requested item is absent from Kodi's library.
// The caller supplies the host selected for its request.
func KodiWatchedSetter(item tracker.Item) func(h *xbmc.XBMCHost, playcount int) {
	if item.Type == tracker.MovieType {
		if m, err := uid.GetMovieByTMDB(item.IDs.TMDB); err == nil && m != nil {
			return func(h *xbmc.XBMCHost, pc int) { h.SetMovieWatched(m.ID, pc, 0, 0) }
		}
		return nil
	}
	show, err := uid.GetShowByTMDB(item.IDs.TMDB)
	if err != nil || show == nil {
		return nil
	}
	switch item.Type {
	case tracker.ShowType:
		return func(h *xbmc.XBMCHost, pc int) { h.SetShowWatched(show.ID, pc) }
	case tracker.SeasonType:
		if se := show.GetSeason(item.Season); se != nil {
			return func(h *xbmc.XBMCHost, pc int) { h.SetSeasonWatched(se.ID, pc) }
		}
	case tracker.EpisodeType:
		if e := show.GetEpisode(item.Season, item.Episode); e != nil {
			return func(h *xbmc.XBMCHost, pc int) { h.SetEpisodeWatched(e.ID, pc, 0, 0) }
		}
	}
	return nil
}
