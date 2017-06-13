package api

import (
	"fmt"
	"log"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/providers"
	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/trakt"
	"github.com/scakemyer/quasar/tmdb"
	"github.com/scakemyer/quasar/xbmc"
)


func TVIndex(ctx *gin.Context) {
	items := xbmc.ListItems{
		{Label: "LOCALIZE[30056]", Path: UrlForXBMC("/shows/trakt/"), Thumbnail: config.AddonResource("img", "trakt.png")},
		{Label: "LOCALIZE[30209]", Path: UrlForXBMC("/shows/search"), Thumbnail: config.AddonResource("img", "search.png")},
		{Label: "LOCALIZE[30246]", Path: UrlForXBMC("/shows/trakt/trending"), Thumbnail: config.AddonResource("img", "trending.png")},
		{Label: "LOCALIZE[30238]", Path: UrlForXBMC("/shows/recent/episodes"), Thumbnail: config.AddonResource("img", "fresh.png")},
		{Label: "LOCALIZE[30237]", Path: UrlForXBMC("/shows/recent/shows"), Thumbnail: config.AddonResource("img", "clock.png")},
		{Label: "LOCALIZE[30210]", Path: UrlForXBMC("/shows/popular"), Thumbnail: config.AddonResource("img", "popular.png")},
		{Label: "LOCALIZE[30211]", Path: UrlForXBMC("/shows/top"), Thumbnail: config.AddonResource("img", "top_rated.png")},
		{Label: "LOCALIZE[30212]", Path: UrlForXBMC("/shows/mostvoted"), Thumbnail: config.AddonResource("img", "most_voted.png")},
		{Label: "LOCALIZE[30289]", Path: UrlForXBMC("/shows/genres"), Thumbnail: config.AddonResource("img", "genre_comedy.png")},
	}
	itemsWithContext := make(xbmc.ListItems, 0)
	for _, item := range items {
		item.ContextMenu = [][]string{
			[]string{"LOCALIZE[30143]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/setviewmode/menus_tvshows"))},
		}
		itemsWithContext = append(itemsWithContext, item)
	}

	ctx.JSON(200, xbmc.NewView("menus_tvshows", itemsWithContext))
}

func TVGenres(ctx *gin.Context) {
	items := make(xbmc.ListItems, 0)
	for _, genre := range tmdb.GetTVGenres(config.Get().Language) {
		slug, _ := genreSlugs[genre.Id]
		items = append(items, &xbmc.ListItem{
			Label:     genre.Name,
			Path:      UrlForXBMC("/shows/popular/%s", strconv.Itoa(genre.Id)),
			Thumbnail: config.AddonResource("img", fmt.Sprintf("genre_%s.png", slug)),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30237]", fmt.Sprintf("Container.Update(%s)", UrlForXBMC("/shows/recent/shows/%s", strconv.Itoa(genre.Id)))},
				[]string{"LOCALIZE[30238]", fmt.Sprintf("Container.Update(%s)", UrlForXBMC("/shows/recent/episodes/%s", strconv.Itoa(genre.Id)))},
				[]string{"LOCALIZE[30144]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/setviewmode/menus_tvshows_genres"))},
			},
		})
	}
	ctx.JSON(200, xbmc.NewView("menus_tvshows_genres", items))
}

func TVTrakt(ctx *gin.Context) {
	items := xbmc.ListItems{
		{Label: "LOCALIZE[30263]", Path: UrlForXBMC("/shows/trakt/lists/"), Thumbnail: config.AddonResource("img", "trakt.png")},
		{
			Label: "LOCALIZE[30254]",
			Path: UrlForXBMC("/shows/trakt/watchlist"),
			Thumbnail: config.AddonResource("img", "trakt.png"),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/library/show/list/add/watchlist"))},
			},
		},
		{
			Label: "LOCALIZE[30257]",
			Path: UrlForXBMC("/shows/trakt/collection"),
			Thumbnail: config.AddonResource("img", "trakt.png"),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/library/show/list/add/collection"))},
			},
		},
		{Label: "LOCALIZE[30290]", Path: UrlForXBMC("/shows/trakt/calendars/"), Thumbnail: config.AddonResource("img", "most_anticipated.png")},
		{Label: "LOCALIZE[30246]", Path: UrlForXBMC("/shows/trakt/trending"), Thumbnail: config.AddonResource("img", "trending.png")},
		{Label: "LOCALIZE[30210]", Path: UrlForXBMC("/shows/trakt/popular"), Thumbnail: config.AddonResource("img", "popular.png")},
		{Label: "LOCALIZE[30247]", Path: UrlForXBMC("/shows/trakt/played"), Thumbnail: config.AddonResource("img", "most_played.png")},
		{Label: "LOCALIZE[30248]", Path: UrlForXBMC("/shows/trakt/watched"), Thumbnail: config.AddonResource("img", "most_watched.png")},
		{Label: "LOCALIZE[30249]", Path: UrlForXBMC("/shows/trakt/collected"), Thumbnail: config.AddonResource("img", "most_collected.png")},
		{Label: "LOCALIZE[30250]", Path: UrlForXBMC("/shows/trakt/anticipated"), Thumbnail: config.AddonResource("img", "most_anticipated.png")},
	}
	ctx.JSON(200, xbmc.NewView("menus_tvshows", items))
}

func TVTraktLists(ctx *gin.Context) {
	items := xbmc.ListItems{}

	for _, list := range trakt.Userlists() {
		item := &xbmc.ListItem{
			Label: list.Name,
			Path:  UrlForXBMC("/shows/trakt/lists/id/%d", list.IDs.Trakt),
			Thumbnail: config.AddonResource("img", "trakt.png"),
			ContextMenu: [][]string{
				[]string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/library/show/list/add/%d", list.IDs.Trakt))},
			},
		}
		items = append(items, item)
	}

	ctx.JSON(200, xbmc.NewView("menus_tvshows", items))
}

func CalendarShows(ctx *gin.Context) {
	items := xbmc.ListItems{
		{Label: "LOCALIZE[30295]", Path: UrlForXBMC("/shows/trakt/calendars/shows"), Thumbnail: config.AddonResource("img", "tv.png")},
		{Label: "LOCALIZE[30296]", Path: UrlForXBMC("/shows/trakt/calendars/newshows"), Thumbnail: config.AddonResource("img", "fresh.png")},
		{Label: "LOCALIZE[30297]", Path: UrlForXBMC("/shows/trakt/calendars/premieres"), Thumbnail: config.AddonResource("img", "box_office.png")},
		{Label: "LOCALIZE[30298]", Path: UrlForXBMC("/shows/trakt/calendars/allshows"), Thumbnail: config.AddonResource("img", "tv.png")},
		{Label: "LOCALIZE[30299]", Path: UrlForXBMC("/shows/trakt/calendars/allnewshows"), Thumbnail: config.AddonResource("img", "fresh.png")},
		{Label: "LOCALIZE[30300]", Path: UrlForXBMC("/shows/trakt/calendars/allpremieres"), Thumbnail: config.AddonResource("img", "box_office.png")},
	}
	ctx.JSON(200, xbmc.NewView("menus_tvshows", items))
}

func renderShows(ctx *gin.Context, shows tmdb.Shows, page int, total int, query string) {
	hasNextPage := 0
	if page > 0 {
		resultsPerPage := config.Get().ResultsPerPage

		if total == -1 {
			total = len(shows)
		}
		if total > resultsPerPage {
			if page * resultsPerPage < total {
				hasNextPage = 1
			}
		}

		if len(shows) > resultsPerPage {
			start := (page - 1) % tmdb.PagesAtOnce * resultsPerPage
			shows = shows[start:start + resultsPerPage]
		}
	}

	items := make(xbmc.ListItems, 0, len(shows) + hasNextPage)

	for _, show := range shows {
		if show == nil {
			continue
		}
		item := show.ToListItem()
		item.Path = UrlForXBMC("/show/%d/seasons", show.Id)

		tmdbId := strconv.Itoa(show.Id)
		libraryAction := []string{"LOCALIZE[30252]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/library/show/add/%d", show.Id))}
		if _, err := isDuplicateShow(tmdbId); err != nil || isAddedToLibrary(tmdbId, Show) {
			libraryAction = []string{"LOCALIZE[30253]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/library/show/remove/%d", show.Id))}
		}
		mergeAction := []string{"LOCALIZE[30283]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/library/show/add/%d?merge=true", show.Id))}

		watchlistAction := []string{"LOCALIZE[30255]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/show/%d/watchlist/add", show.Id))}
		if inShowsWatchlist(show.Id) {
			watchlistAction = []string{"LOCALIZE[30256]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/show/%d/watchlist/remove", show.Id))}
		}

		collectionAction := []string{"LOCALIZE[30258]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/show/%d/collection/add", show.Id))}
		if inShowsCollection(show.Id) {
			collectionAction = []string{"LOCALIZE[30259]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/show/%d/collection/remove", show.Id))}
		}

		item.ContextMenu = [][]string{
			libraryAction,
			mergeAction,
			watchlistAction,
			collectionAction,
			[]string{"LOCALIZE[30035]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/setviewmode/tvshows"))},
		}
		if config.Get().Platform.Kodi < 17 {
			item.ContextMenu = append(item.ContextMenu, []string{"LOCALIZE[30203]", "XBMC.Action(Info)"})
		}
		items = append(items, item)
	}
	if page >= 0 && hasNextPage > 0 {
		path := ctx.Request.URL.Path
		nextPath := UrlForXBMC(fmt.Sprintf("%s?page=%d", path, page + 1))
		if query != "" {
			nextPath = UrlForXBMC(fmt.Sprintf("%s?q=%s&page=%d", path, query, page + 1))
		}
		next := &xbmc.ListItem{
			Label: "LOCALIZE[30218]",
			Path: nextPath,
			Thumbnail: config.AddonResource("img", "nextpage.png"),
		}
		items = append(items, next)
	}
	ctx.JSON(200, xbmc.NewView("tvshows", items))
}

func PopularShows(ctx *gin.Context) {
	genre := ctx.Params.ByName("genre")
	if genre == "0" {
		genre = ""
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.PopularShows(genre, config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

func RecentShows(ctx *gin.Context) {
	genre := ctx.Params.ByName("genre")
	if genre == "0" {
		genre = ""
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.RecentShows(genre, config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

func RecentEpisodes(ctx *gin.Context) {
	genre := ctx.Params.ByName("genre")
	if genre == "0" {
		genre = ""
	}
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.RecentEpisodes(genre, config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

func TopRatedShows(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.TopRatedShows("", config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

func TVMostVoted(ctx *gin.Context) {
	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.MostVotedShows("", config.Get().Language, page)
	renderShows(ctx, shows, page, total, "")
}

func SearchShows(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	query := ctx.Query("q")
	keyboard := ctx.Query("keyboard")

	if len(query) == 0 {
		historyType := "shows"
		if len(keyboard) > 0 || searchHistoryEmpty(historyType) {
			query = xbmc.Keyboard("", "LOCALIZE[30206]")
			if len(query) == 0 {
				return
			}
			searchHistoryAppend(ctx, historyType, query)
		} else if !searchHistoryEmpty(historyType) {
			searchHistoryList(ctx, historyType)
		}
		return
	}

	page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
	shows, total := tmdb.SearchShows(query, config.Get().Language, page)
	renderShows(ctx, shows, page, total, query)
}

func ShowSeasons(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	showId, _ := strconv.Atoi(ctx.Params.ByName("showId"))

	show := tmdb.GetShow(showId, config.Get().Language)

	if show == nil {
		ctx.Error(errors.New("Unable to find show"))
		return
	}

	items := show.Seasons.ToListItems(show)
	reversedItems := make(xbmc.ListItems, 0)
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		item.Path = UrlForXBMC("/show/%d/season/%d/episodes", show.Id, item.Info.Season)
		item.ContextMenu = [][]string{
			[]string{"LOCALIZE[30202]", fmt.Sprintf("XBMC.PlayMedia(%s)", UrlForXBMC("/show/%d/season/%d/links", show.Id, item.Info.Season))},
			[]string{"LOCALIZE[30036]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/setviewmode/seasons"))},
		}
		reversedItems = append(reversedItems, item)
	}
	// xbmc.ListItems always returns false to Less() so that order is unchanged

	ctx.JSON(200, xbmc.NewView("seasons", reversedItems))
}

func ShowEpisodes(ctx *gin.Context) {
	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	showId, _ := strconv.Atoi(ctx.Params.ByName("showId"))
	seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
	language := config.Get().Language

	show := tmdb.GetShow(showId, language)
	if show == nil {
		ctx.Error(errors.New("Unable to find show"))
		return
	}

	season := tmdb.GetSeason(showId, seasonNumber, language)
	if season == nil {
		ctx.Error(errors.New("Unable to find season"))
		return
	}

	items := season.Episodes.ToListItems(show, season)

	for _, item := range items {
		playLabel := "LOCALIZE[30023]"
		playURL := UrlForXBMC("/show/%d/season/%d/episode/%d/play",
			show.Id,
			seasonNumber,
			item.Info.Episode,
		)
		linksLabel := "LOCALIZE[30202]"
		linksURL := UrlForXBMC("/show/%d/season/%d/episode/%d/links",
			show.Id,
			seasonNumber,
			item.Info.Episode,
		)

		defaultURL := linksURL
		contextLabel := playLabel
		contextURL := playURL
		if config.Get().ChooseStreamAuto == true {
			defaultURL = playURL
			contextLabel = linksLabel
			contextURL = linksURL
		}

		item.Path = defaultURL

		if config.Get().Platform.Kodi < 17 {
			item.ContextMenu = [][]string{
				[]string{contextLabel, fmt.Sprintf("XBMC.PlayMedia(%s)", contextURL)},
				[]string{"LOCALIZE[30203]", "XBMC.Action(Info)"},
				[]string{"LOCALIZE[30268]", "XBMC.Action(ToggleWatched)"},
				[]string{"LOCALIZE[30037]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/setviewmode/episodes"))},
			}
		} else {
			item.ContextMenu = [][]string{
				[]string{contextLabel, fmt.Sprintf("XBMC.PlayMedia(%s)", contextURL)},
				[]string{"LOCALIZE[30037]", fmt.Sprintf("XBMC.RunPlugin(%s)", UrlForXBMC("/setviewmode/episodes"))},
			}
		}
		item.IsPlayable = true
	}

	ctx.JSON(200, xbmc.NewView("episodes", items))
}

func showSeasonLinks(showId int, seasonNumber int) ([]*bittorrent.TorrentFile, error) {
	log.Println("Searching links for TMDB Id:", showId)

	show := tmdb.GetShow(showId, config.Get().Language)
	if show == nil {
		return nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showId, seasonNumber, config.Get().Language)
	if season == nil {
		return nil, errors.New("Unable to find season")
	}

	log.Printf("Resolved %d to %s", showId, show.Name)

	searchers := providers.GetSeasonSearchers()
	if len(searchers) == 0 {
		xbmc.Notify("Quasar", "LOCALIZE[30204]", config.AddonIcon())
	}

	return providers.SearchSeason(searchers, show, season), nil
}

func ShowSeasonLinks(btService *bittorrent.BTService, fromLibrary bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		showId, _ := strconv.Atoi(ctx.Params.ByName("showId"))
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		external := ctx.Query("external")
		library := ""
		if fromLibrary {
			library = "1"
		}

		show := tmdb.GetShow(showId, "")
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		season := tmdb.GetSeason(showId, seasonNumber,"")
		if season == nil {
			ctx.Error(errors.New("Unable to find season"))
			return
		}

		longName := fmt.Sprintf("%s Season %02d", show.Name, seasonNumber)

		existingTorrent := ExistingTorrent(btService, longName)
		if existingTorrent != "" && xbmc.DialogConfirm("Quasar", "LOCALIZE[30270]") {
			rUrl := UrlQuery(
				UrlForXBMC("/play"), "uri", existingTorrent,
				                     "tmdb", strconv.Itoa(season.Id),
				                     "library", library,
				                     "type", "episode")
			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
			return
		}

		if torrents := InTorrentsMap(strconv.Itoa(season.Id)); len(torrents) > 0 {
			rUrl := UrlQuery(
				UrlForXBMC("/play"), "uri", torrents[0].URI,
				                     "tmdb", strconv.Itoa(season.Id),
				                     "library", library,
				                     "type", "episode")
			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
			return
		}

		torrents, err := showSeasonLinks(showId, seasonNumber)
		if err != nil {
			ctx.Error(err)
			return
		}

		if len(torrents) == 0 {
			xbmc.Notify("Quasar", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		choices := make([]string, 0, len(torrents))
		for _, torrent := range torrents {
			resolution := ""
			if torrent.Resolution > 0 {
				resolution = fmt.Sprintf("[B][COLOR %s]%s[/COLOR][/B] ", bittorrent.Colors[torrent.Resolution], bittorrent.Resolutions[torrent.Resolution])
			}

			info := make([]string, 0)
			if torrent.Size != "" {
				info = append(info, fmt.Sprintf("[B][%s][/B]", torrent.Size))
			}
			if torrent.RipType > 0 {
				info = append(info, bittorrent.Rips[torrent.RipType])
			}
			if torrent.VideoCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.VideoCodec])
			}
			if torrent.AudioCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.AudioCodec])
			}
			if torrent.Provider != "" {
				info = append(info, fmt.Sprintf(" - [B]%s[/B]", torrent.Provider))
			}

			multi := ""
			if torrent.Multi {
				multi = "\nmulti"
			}

			label := fmt.Sprintf("%s(%d / %d) %s\n%s\n%s%s",
				resolution,
				torrent.Seeds,
				torrent.Peers,
				strings.Join(info, " "),
				torrent.Name,
				torrent.Icon,
				multi,
			)
			choices = append(choices, label)
		}

		choice := xbmc.ListDialogLarge("LOCALIZE[30228]", longName, choices...)
		if choice >= 0 {
			AddToTorrentsMap(strconv.Itoa(season.Id), torrents[choice])

			rUrl := UrlQuery(UrlForXBMC("/play"), "uri", torrents[choice].URI)

			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
		}
	}
}

func showEpisodeLinks(showId int, seasonNumber int, episodeNumber int) ([]*bittorrent.TorrentFile, error) {
	log.Println("Searching links for TMDB Id:", showId)

	show := tmdb.GetShow(showId, config.Get().Language)
	if show == nil {
		return nil, errors.New("Unable to find show")
	}

	season := tmdb.GetSeason(showId, seasonNumber, config.Get().Language)
	if season == nil {
		return nil, errors.New("Unable to find season")
	}

	episode := season.Episodes[episodeNumber - 1]

	log.Printf("Resolved %d to %s", showId, show.Name)

	searchers := providers.GetEpisodeSearchers()
	if len(searchers) == 0 {
		xbmc.Notify("Quasar", "LOCALIZE[30204]", config.AddonIcon())
	}

	return providers.SearchEpisode(searchers, show, episode), nil
}

func ShowEpisodePlaySelector(link string, btService *bittorrent.BTService, fromLibrary bool) gin.HandlerFunc {
	play := link == "play"

	if config.Get().ForceLinkType {
		if config.Get().ChooseStreamAuto {
			play = true
		} else {
			play = false
		}
	}

	if play {
		return ShowEpisodePlay(btService, fromLibrary)
	} else {
		return ShowEpisodeLinks(btService, fromLibrary)
	}
}

func ShowEpisodeLinks(btService *bittorrent.BTService, fromLibrary bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		tmdbId := ctx.Params.ByName("showId")
		showId, _ := strconv.Atoi(tmdbId)
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))
		external := ctx.Query("external")
		library := ""
		if fromLibrary {
			library = "1"
		}

		show := tmdb.GetShow(showId, "")
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		episode := tmdb.GetEpisode(showId, seasonNumber, episodeNumber, "")
		if episode == nil {
			ctx.Error(errors.New("Unable to find episode"))
			return
		}

		longName := fmt.Sprintf("%s S%02dE%02d", show.Name, seasonNumber, episodeNumber)

		existingTorrent := ExistingTorrent(btService, longName)
		if existingTorrent != "" && xbmc.DialogConfirm("Quasar", "LOCALIZE[30270]") {
			rUrl := UrlQuery(
				UrlForXBMC("/play"), "uri", existingTorrent,
				                     "tmdb", strconv.Itoa(episode.Id),
				                     "show", tmdbId,
				                     "season", ctx.Params.ByName("season"),
				                     "episode", ctx.Params.ByName("episode"),
				                     "library", library,
				                     "type", "episode")
			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
			return
		}

		if torrents := InTorrentsMap(strconv.Itoa(episode.Id)); len(torrents) > 0 {
			rUrl := UrlQuery(
				UrlForXBMC("/play"), "uri", torrents[0].URI,
				                     "tmdb", strconv.Itoa(episode.Id),
				                     "show", tmdbId,
				                     "season", ctx.Params.ByName("season"),
				                     "episode", ctx.Params.ByName("episode"),
				                     "library", library,
				                     "type", "episode")
			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
			return
		}

		torrents, err := showEpisodeLinks(showId, seasonNumber, episodeNumber)
		if err != nil {
			ctx.Error(err)
			return
		}

		if len(torrents) == 0 {
			xbmc.Notify("Quasar", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		choices := make([]string, 0, len(torrents))
		for _, torrent := range torrents {
			resolution := ""
			if torrent.Resolution > 0 {
				resolution = fmt.Sprintf("[B][COLOR %s]%s[/COLOR][/B] ", bittorrent.Colors[torrent.Resolution], bittorrent.Resolutions[torrent.Resolution])
			}

			info := make([]string, 0)
			if torrent.Size != "" {
				info = append(info, fmt.Sprintf("[B][%s][/B]", torrent.Size))
			}
			if torrent.RipType > 0 {
				info = append(info, bittorrent.Rips[torrent.RipType])
			}
			if torrent.VideoCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.VideoCodec])
			}
			if torrent.AudioCodec > 0 {
				info = append(info, bittorrent.Codecs[torrent.AudioCodec])
			}
			if torrent.Provider != "" {
				info = append(info, fmt.Sprintf(" - [B]%s[/B]", torrent.Provider))
			}

			multi := ""
			if torrent.Multi {
				multi = "\nmulti"
			}

			label := fmt.Sprintf("%s(%d / %d) %s\n%s\n%s%s",
				resolution,
				torrent.Seeds,
				torrent.Peers,
				strings.Join(info, " "),
				torrent.Name,
				torrent.Icon,
				multi,
			)
			choices = append(choices, label)
		}

		choice := xbmc.ListDialogLarge("LOCALIZE[30228]", longName, choices...)
		if choice >= 0 {
			AddToTorrentsMap(strconv.Itoa(episode.Id), torrents[choice])

			rUrl := UrlQuery(
				UrlForXBMC("/play"), "uri", torrents[choice].URI,
				                     "tmdb", strconv.Itoa(episode.Id),
				                     "show", tmdbId,
				                     "season", ctx.Params.ByName("season"),
				                     "episode", ctx.Params.ByName("episode"),
				                     "library", library,
				                     "type", "episode")
			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
		}
	}
}

func ShowEpisodePlay(btService *bittorrent.BTService, fromLibrary bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")

		tmdbId := ctx.Params.ByName("showId")
		showId, _ := strconv.Atoi(tmdbId)
		seasonNumber, _ := strconv.Atoi(ctx.Params.ByName("season"))
		episodeNumber, _ := strconv.Atoi(ctx.Params.ByName("episode"))
		external := ctx.Query("external")
		library := ""
		if fromLibrary {
			library = "1"
		}

		show := tmdb.GetShow(showId, "")
		if show == nil {
			ctx.Error(errors.New("Unable to find show"))
			return
		}

		episode := tmdb.GetEpisode(showId, seasonNumber, episodeNumber, "")
		if episode == nil {
			ctx.Error(errors.New("Unable to find episode"))
			return
		}

		longName := fmt.Sprintf("%s S%02dE%02d", show.Name, seasonNumber, episodeNumber)
		existingTorrent := ExistingTorrent(btService, longName)
		if existingTorrent != "" && xbmc.DialogConfirm("Quasar", "LOCALIZE[30270]") {
			rUrl := UrlQuery(
				UrlForXBMC("/play"), "uri", existingTorrent,
				                     "tmdb", strconv.Itoa(episode.Id),
				                     "show", tmdbId,
				                     "season", ctx.Params.ByName("season"),
				                     "episode", ctx.Params.ByName("episode"),
				                     "library", library,
				                     "type", "episode")
			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
			return
		}

		if torrents := InTorrentsMap(strconv.Itoa(episode.Id)); len(torrents) > 0 {
			rUrl := UrlQuery(
				UrlForXBMC("/play"), "uri", torrents[0].URI,
				                     "tmdb", strconv.Itoa(episode.Id),
				                     "show", tmdbId,
				                     "season", ctx.Params.ByName("season"),
				                     "episode", ctx.Params.ByName("episode"),
				                     "library", library,
				                     "type", "episode")
			if external != "" {
				xbmc.PlayURL(rUrl)
			} else {
				ctx.Redirect(302, rUrl)
			}
			return
		}

		torrents, err := showEpisodeLinks(showId, seasonNumber, episodeNumber)
		if err != nil {
			ctx.Error(err)
			return
		}

		if len(torrents) == 0 {
			xbmc.Notify("Quasar", "LOCALIZE[30205]", config.AddonIcon())
			return
		}

		AddToTorrentsMap(strconv.Itoa(episode.Id), torrents[0])

		rUrl := UrlQuery(
			UrlForXBMC("/play"), "uri", torrents[0].URI,
			                     "tmdb", strconv.Itoa(episode.Id),
			                     "show", tmdbId,
			                     "season", ctx.Params.ByName("season"),
			                     "episode", ctx.Params.ByName("episode"),
			                     "library", library,
			                     "type", "episode")
		if external != "" {
			xbmc.PlayURL(rUrl)
		} else {
			ctx.Redirect(302, rUrl)
		}
	}
}
