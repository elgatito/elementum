package api

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/trakt"
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

		activities, err := tracker.GetActivities(p, fmt.Sprintf("view.%s.%d", kind, t))
		refresh := err != nil || activities.Changed(bucket)

		var items []*tracker.Item
		listID := ctx.Params.ByName("listId")
		switch kind {
		case tracker.KindWatchlist:
			items, err = p.Watchlist(t, refresh)
		case tracker.KindCollection:
			if c, ok := p.(tracker.Collector); ok {
				items, err = c.Collection(t, refresh)
			}
		case tracker.KindList:
			if l, ok := p.(tracker.ListReader); ok {
				items, err = l.ListItems(listID, t, refresh)
			}
		}
		if err != nil {
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
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

// trackerIcon is the menu icon of the active tracker; only Trakt has its own.
func trackerIcon() string {
	if tracker.IsActive("trakt") {
		return config.AddonResource("img", "trakt.png")
	}
	return config.AddonResource("img", "favourites.png")
}

// trackerListsView lists user lists of the active tracker.
func trackerListsView(media string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		items := xbmc.ListItems{}
		if l, ok := tracker.Active().(tracker.ListReader); ok {
			lists, err := l.Lists()
			if err != nil {
				log.Warningf("Could not get tracker lists: %s", err)
			}
			for _, list := range lists {
				items = append(items, &xbmc.ListItem{
					Label:     list.Name,
					Path:      URLForXBMC("/%ss/tracker/lists/%s", media, list.ID),
					Thumbnail: trackerIcon(),
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

// trackerMenuItems returns menu entries for the active tracker's lists, labeled with its name.
func trackerMenuItems(media string) xbmc.ListItems {
	p := tracker.Active()
	if p == nil {
		return nil
	}

	items := xbmc.ListItems{}
	if _, ok := p.(tracker.ListReader); ok {
		items = append(items, &xbmc.ListItem{Label: p.Name() + " > LOCALIZE[30263]", Path: URLForXBMC("/%ss/tracker/lists/", media), Thumbnail: trackerIcon()})
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
			Thumbnail:   trackerIcon(),
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
