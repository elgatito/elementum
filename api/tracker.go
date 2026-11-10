package api

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/ui"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

// trackerItem builds a tracker item from a TMDB id taken from a route.
func trackerItem(t tracker.MediaType, tmdbID string) *tracker.Item {
	id, _ := strconv.Atoi(tmdbID)
	return &tracker.Item{Type: t, IDs: tracker.IDs{TMDB: id}}
}

// trackerView serves a Trakt view when Trakt is the active tracker, and the generic one otherwise.
func trackerView(traktView, genericView gin.HandlerFunc) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if tracker.IsActive("trakt") {
			traktView(ctx)
		} else {
			genericView(ctx)
		}
	}
}

// trackerListView renders a watchlist, collection or user list of the active tracker.
func trackerListView(kind tracker.ListKind, t tracker.MediaType, bucket tracker.Bucket) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
		if xbmcHost == nil {
			return
		}

		p := tracker.Active()
		if p == nil {
			xbmcHost.Notify("Elementum", "LOCALIZE[30795]", config.AddonIcon())
			return
		}

		items, err := tracker.ViewItems(p, kind, ctx.Params.ByName("listId"), t, bucket)
		if err != nil {
			notifyTrackerError(xbmcHost, p, err)
		}

		page, _ := strconv.Atoi(ctx.DefaultQuery("page", "0"))
		// Trakt views render from TMDB ids, so tracker items reuse them through minimal Trakt objects
		if t == tracker.MovieType {
			movies := make([]*trakt.Movies, 0, len(items))
			for _, i := range items {
				movies = append(movies, &trakt.Movies{Movie: &trakt.Movie{Object: trakt.Object{Title: i.Title, Year: i.Year, IDs: &trakt.IDs{TMDB: i.IDs.TMDB, IMDB: i.IDs.IMDB, TVDB: i.IDs.TVDB}}}})
			}
			renderTraktMovies(ctx, movies, -1, page)
			return
		}

		shows := make([]*trakt.Shows, 0, len(items))
		for _, i := range items {
			shows = append(shows, &trakt.Shows{Show: &trakt.Show{Object: trakt.Object{Title: i.Title, Year: i.Year, IDs: &trakt.IDs{TMDB: i.IDs.TMDB, IMDB: i.IDs.IMDB, TVDB: i.IDs.TVDB}}}})
		}
		renderTraktShows(ctx, shows, -1, page)
	}
}

// trackerListsView lists user lists of the active tracker.
func trackerListsView(media string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
		if xbmcHost == nil {
			return
		}

		items := xbmc.ListItems{}
		p := tracker.Active()
		if l, ok := p.(tracker.ListReader); ok {
			lists, err := l.Lists()
			if err != nil {
				notifyTrackerError(xbmcHost, p, err)
			}
			for _, list := range lists {
				items = append(items, &xbmc.ListItem{
					Label:     list.Name,
					Path:      URLForXBMC("/%ss/tracker/lists/%s", media, list.ID),
					Thumbnail: config.AddonResource("img", "lists.png"),
					ContextMenu: [][]string{
						{"LOCALIZE[30252]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/%s/list/add/%s", media, list.ID))},
					},
				})
			}
		}

		view := "menus_movies"
		if media == showType {
			view = "menus_tvshows"
		}
		ctx.JSON(200, xbmc.NewView(view, items))
	}
}

// trackerProgressView renders the next episode of every show in progress on the active tracker.
func trackerProgressView(ctx *gin.Context) {
	defer perf.ScopeTimer()()

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	p := tracker.Active()
	pr, ok := p.(tracker.ProgressReader)
	if !ok {
		xbmcHost.Notify("Elementum", "LOCALIZE[30795]", config.AddonIcon())
		return
	}

	activities, err := tracker.GetActivities(p, "view.progress")
	items, err := pr.Progress(err != nil || activities.Changed(tracker.EpisodesWatched))
	if err != nil {
		notifyTrackerError(xbmcHost, p, err)
	}

	// the progress renderer reads TMDB ids and episode numbers, so items travel as minimal Trakt objects
	shows := make([]*trakt.ProgressShow, 0, len(items))
	for _, i := range items {
		shows = append(shows, &trakt.ProgressShow{
			Show:    &trakt.Show{Object: trakt.Object{Title: i.Title, Year: i.Year, IDs: &trakt.IDs{TMDB: i.IDs.TMDB, IMDB: i.IDs.IMDB, TVDB: i.IDs.TVDB}}},
			Episode: &trakt.Episode{Season: i.Season, Number: i.Episode, IDs: &trakt.IDs{TMDB: i.EpisodeTMDB}},
		})
	}
	renderProgressShows(ctx, shows, -1, 0)
}

// notifyTrackerError tells the user why a tracker view is empty.
func notifyTrackerError(xbmcHost *xbmc.XBMCHost, p tracker.Provider, err error) {
	msg := err.Error()
	if errors.Is(err, tracker.ErrPaidAccount) && xbmcHost != nil {
		msg = fmt.Sprintf(xbmcHost.GetLocalizedString(30806), p.Name())
	} else if errors.Is(err, tracker.ErrNotAuthorized) || errors.Is(err, util.ErrUnauthorized) {
		msg = "LOCALIZE[30149]"
	}
	if xbmcHost == nil {
		ui.Toast(msg)
		return
	}
	xbmcHost.Notify("Elementum", msg, config.AddonIcon())
}

// traktListsView lists the Trakt user and liked lists of movies or shows. media is the /movies/ or /shows/
// route group, menu the /menu/<menu>/ route that pins a list to the addon menu.
func traktListsView(media, menu, view string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
		items := xbmc.ListItems{}
		lists, err := trakt.Userlists()
		if err != nil {
			log.Error(err)
			notifyTrackerError(xbmcHost, trakt.NewProvider(), err)
		}
		lists = append(lists, trakt.Likedlists()...)
		lists = slices.DeleteFunc(lists, func(list *trakt.List) bool { return list == nil || list.User == nil })

		slices.SortFunc(lists, func(a, b *trakt.List) int {
			return cmp.Compare(a.Name, b.Name)
		})

		for _, list := range lists {
			link := URLForXBMC("/%s/trakt/lists/%s/%d", media, list.Username(), list.ID())
			menuItem := []string{"LOCALIZE[30520]", fmt.Sprintf("RunPlugin(%s)", URLQuery(URLForXBMC("/menu/%s/add", menu), "name", list.Name, "link", link))}
			if MovieMenu.Contains(addAction, &MenuItem{Name: list.Name, Link: link}) {
				menuItem = []string{"LOCALIZE[30521]", fmt.Sprintf("RunPlugin(%s)", URLQuery(URLForXBMC("/menu/%s/remove", menu), "name", list.Name, "link", link))}
			}

			items = append(items, &xbmc.ListItem{
				Label:       list.Name,
				Path:        link,
				Thumbnail:   config.AddonResource("img", "lists.png"),
				ContextMenu: [][]string{menuItem},
			})
		}
		ctx.JSON(200, xbmc.NewView(view, filterListItems(items)))
	}
}

// trackerMenuItems returns menu entries for the active tracker's lists, labeled with its name.
func trackerMenuItems(media string) xbmc.ListItems {
	p := tracker.Active()
	if p == nil {
		return nil
	}

	items := xbmc.ListItems{}
	if _, ok := p.(tracker.ProgressReader); ok && media == showType {
		items = append(items, &xbmc.ListItem{Label: p.Name() + " > LOCALIZE[30360]", Path: URLForXBMC("/shows/tracker/progress"), Thumbnail: config.AddonResource("img", "progress.png")})
	}
	if _, ok := p.(tracker.ListReader); ok {
		items = append(items, &xbmc.ListItem{Label: p.Name() + " > LOCALIZE[30263]", Path: URLForXBMC("/%ss/tracker/lists/", media), Thumbnail: config.AddonResource("img", "lists.png")})
	}
	lists := []string{"watchlist"}
	labels := map[string]string{"watchlist": "LOCALIZE[30254]", "collection": "LOCALIZE[30257]"}
	if _, ok := p.(tracker.Collector); ok {
		lists = append(lists, "collection")
	}
	for _, list := range lists {
		items = append(items, &xbmc.ListItem{
			Label:       p.Name() + " > " + labels[list],
			Path:        URLForXBMC("/%ss/tracker/%s", media, list),
			Thumbnail:   config.AddonResource("img", list+".png"), // watchlist.png, collection.png
			ContextMenu: [][]string{{"LOCALIZE[30252]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/library/%s/list/add/%s", media, list))}},
		})
	}
	return items
}

// trackerContextMenu drops collection actions when the active tracker has no collection.
func trackerContextMenu(menu [][]string) [][]string {
	if tracker.Active() == nil || tracker.HasCollection() {
		return menu
	}
	return slices.DeleteFunc(menu, func(m []string) bool { return len(m) > 1 && strings.Contains(m[1], "/collection/") })
}

// settingsTracker returns the tracker selected in Kodi settings right now. Settings actions run before
// the daemon reloads its configuration, so config.Get() can still hold the previous selection.
func settingsTracker(xbmcHost *xbmc.XBMCHost) tracker.Provider {
	if i := xbmcHost.GetSettingInt("tracker_provider"); i > 0 && i < len(config.TrackerProviders) {
		return tracker.Get(config.TrackerProviders[i])
	}
	return tracker.Selected()
}

var (
	MoviesTraktLists = traktListsView("movies", "movie", "menus_movies")
	TVTraktLists     = traktListsView("shows", "shows", "menus_tvshows")
)
