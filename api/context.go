package api

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

// ContextPlaySelector plays/downloads/toggles_watched media from Kodi in elementum
func ContextPlaySelector(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		action := ctx.Params.ByName("action")
		id := ctx.Params.ByName("kodiID")
		kodiID, _ := strconv.Atoi(id)
		media := ctx.Params.ByName("media")

		mediaAction := "forcelinks"
		if media == "movie" && config.Get().ChooseStreamAutoMovie {
			mediaAction = "forceplay"
		} else if (media == "episode" || media == "season") && config.Get().ChooseStreamAutoShow {
			mediaAction = "forceplay"
		} else if kodiID == 0 && config.Get().ChooseStreamAutoSearch {
			mediaAction = "forceplay"
		}

		if action == "download" || action == "watched" || action == "unwatched" {
			mediaAction = action
		}

		if kodiID == 0 {
			if mediaAction != "watched" && mediaAction != "unwatched" {
				ctx.Redirect(302, URLQuery(URLForXBMC("/search"), "q", id, "action", mediaAction))
			} else {
				log.Error("Can't set %q for non-library item of type %q: %q", mediaAction, media, id)
			}
			return
		} else if media == "movie" {
			if m := library.GetLibraryMovie(kodiID); m != nil && m.UIDs.TMDB != 0 {
				title := fmt.Sprintf("%s (%d)", m.Title, m.Year)
				ctx.Redirect(302, URLQuery(URLForXBMC("/movie/%d/%s/%s", m.UIDs.TMDB, mediaAction, url.PathEscape(title))))
				return
			}
		} else if media == "episode" {
			if s, e := library.GetLibraryEpisode(kodiID); s != nil && e != nil && s.UIDs.TMDB != 0 {
				title := fmt.Sprintf("%s S%02dE%02d", s.Title, e.Season, e.Episode)
				ctx.Redirect(302, URLQuery(URLForXBMC("/show/%d/season/%d/episode/%d/%s/%s", s.UIDs.TMDB, e.Season, e.Episode, mediaAction, url.PathEscape(title))))
				return
			}
		} else if media == "season" {
			if s, se := library.GetLibrarySeason(kodiID); s != nil && se != nil && s.UIDs.TMDB != 0 {
				title := fmt.Sprintf("%s S%02d", s.Title, se.Season)
				ctx.Redirect(302, URLQuery(URLForXBMC("/show/%d/season/%d/%s/%s", s.UIDs.TMDB, se.Season, mediaAction, url.PathEscape(title))))
				return
			}
		} else if media == "tvshow" {
			if s := library.GetLibraryShow(kodiID); s != nil && s.UIDs.TMDB != 0 {
				title := fmt.Sprintf("%s", s.Title)
				ctx.Redirect(302, URLQuery(URLForXBMC("/show/%d/%s/%s", s.UIDs.TMDB, mediaAction, url.PathEscape(title))))
				return
			}
		}

		err := fmt.Errorf("Cound not find TMDB entry for requested Kodi item %d of type %s", kodiID, media)
		log.Error(err.Error())
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.Error(errors.New("Cannot find TMDB for selected Kodi item"))
		return
	}
}

// ContextAssignKodiSelector assigns torrent to movie/episode by Kodi library ID
func ContextAssignKodiSelector(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		id := ctx.Params.ByName("kodiID")
		kodiID, _ := strconv.Atoi(id)
		media := ctx.Params.ByName("media")

		var tmdbID int

		if kodiID != 0 {
			if media == "movie" {
				if m := library.GetLibraryMovie(kodiID); m != nil && m.UIDs.TMDB != 0 {
					tmdbID = m.UIDs.TMDB
					ctx.Redirect(302, URLQuery(URLForXBMC("/context/torrents/assign/%s/tmdb/%s/%d", torrentID, media, tmdbID)))
					return
				}
			} else if media == "episode" {
				if s, e := library.GetLibraryEpisode(kodiID); s != nil && e != nil && s.UIDs.TMDB != 0 {
					tmdbID = s.UIDs.TMDB
					ctx.Redirect(302, URLQuery(URLForXBMC("/context/torrents/assign/%s/tmdb/show/%d/season/%d/%s/%d", torrentID, tmdbID, e.Season, media, e.Episode)))
					return
				}
			} else if media == "season" {
				if s, se := library.GetLibrarySeason(kodiID); s != nil && se != nil && s.UIDs.TMDB != 0 {
					tmdbID = s.UIDs.TMDB
					ctx.Redirect(302, URLQuery(URLForXBMC("/context/torrents/assign/%s/tmdb/show/%d/%s/%d", torrentID, tmdbID, media, se.Season)))
					return
				}
			}
		}

		err := fmt.Errorf("Cound not find TMDB entry for requested Kodi item %d of type %s", kodiID, media)
		log.Error(err.Error())
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.Error(errors.New("Cannot find TMDB for selected Kodi item"))
		return
	}
}

// ContextAssignTMDBMovieSelector assigns torrent to movie by TMDB ID
func ContextAssignTMDBMovieSelector(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		id := ctx.Params.ByName("tmdbId")
		tmdbID, _ := strconv.Atoi(id)
		media := ctx.Params.ByName("media")

		if tmdbID != 0 {
			ctx.Redirect(302, URLQuery(URLForXBMC("/torrents/assign/%s/%d", torrentID, tmdbID)))
			return
		}

		err := fmt.Errorf("Cound not find TMDB entry for requested Kodi item %d of type %s", tmdbID, media)
		log.Error(err.Error())
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.Error(errors.New("Cannot find TMDB for selected Kodi item"))
		return
	}
}

// ContextAssignTMDBSeasonSelector assigns torrent to season by show TMDB ID and season number
func ContextAssignTMDBSeasonSelector(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		id := ctx.Params.ByName("tmdbId")
		showID, _ := strconv.Atoi(id)
		media := ctx.Params.ByName("media")
		seasonN := ctx.Params.ByName("season")
		seasonNumber, _ := strconv.Atoi(seasonN)

		if showID != 0 && seasonNumber != 0 {
			season := tmdb.GetSeason(showID, seasonNumber, config.Get().Language, 0)
			if season == nil {
				err := errors.New("Unable to find season")
				log.Error(err.Error())
				xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
				ctx.Error(err)
				return
			}

			ctx.Redirect(302, URLQuery(URLForXBMC("/torrents/assign/%s/%d", torrentID, season.ID)))
			return
		}

		err := fmt.Errorf("Cound not find TMDB entry for requested Kodi item %d of type %s #%d", showID, media, seasonNumber)
		log.Error(err.Error())
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.Error(errors.New("Cannot find TMDB for selected Kodi item"))
		return
	}
}

// ContextAssignTMDBEpisodeSelector assigns torrent to episode by show TMDB ID and season/episode numbers
func ContextAssignTMDBEpisodeSelector(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		torrentID := ctx.Params.ByName("torrentId")
		id := ctx.Params.ByName("tmdbId")
		showID, _ := strconv.Atoi(id)
		media := ctx.Params.ByName("media")
		seasonN := ctx.Params.ByName("season")
		seasonNumber, _ := strconv.Atoi(seasonN)
		episodeN := ctx.Params.ByName("episode")
		episodeNumber, _ := strconv.Atoi(episodeN)

		if showID != 0 && seasonNumber != 0 {
			episode := tmdb.GetEpisode(showID, seasonNumber, episodeNumber, config.Get().Language)
			if episode == nil {
				err := errors.New("Unable to find episode")
				log.Error(err.Error())
				xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
				ctx.Error(err)
				return
			}

			ctx.Redirect(302, URLQuery(URLForXBMC("/torrents/assign/%s/%d", torrentID, episode.ID)))
			return
		}

		err := fmt.Errorf("Cound not find TMDB entry for requested Kodi item %d of type %s S%dE%d", showID, media, seasonNumber, episodeNumber)
		log.Error(err.Error())
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.Error(errors.New("Cannot find TMDB for selected Kodi item"))
		return
	}
}

// ContextActionFromKodiLibrarySelector does action for media in Kodi library (by Kodi library ID)
func ContextActionFromKodiLibrarySelector(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		action := ctx.Params.ByName("action")
		id := ctx.Params.ByName("kodiID")
		kodiID, _ := strconv.Atoi(id)
		media := ctx.Params.ByName("media")

		var tmdbID int

		if kodiID != 0 {
			if media == "movie" {
				if m := library.GetLibraryMovie(kodiID); m != nil && m.UIDs.TMDB != 0 {
					tmdbID = m.UIDs.TMDB
				}
			} else if media == "show" {
				if s := library.GetLibraryShow(kodiID); s != nil && s.UIDs.TMDB != 0 {
					tmdbID = s.UIDs.TMDB
				}
			} else {
				err := fmt.Errorf("Unsupported media type: %s", media)
				xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
				ctx.Error(err)
				return
			}
			ctx.Redirect(302, URLQuery(URLForXBMC("/library/%s/%s/%d", media, action, tmdbID)))
			return
		}

		err := fmt.Errorf("Cound not find TMDB entry for requested Kodi item %d of type %s", kodiID, media)
		log.Error(err.Error())
		xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
		ctx.Error(errors.New("Cannot find TMDB for selected Kodi item"))
		return
	}
}
