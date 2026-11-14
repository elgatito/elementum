package jsonapi

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/tracker"
	"github.com/elgatito/elementum/trakt"
	"github.com/elgatito/elementum/util"
)

// Tracker lists come whole from the tracker layer (which caches them) and are paged here; only the page's
// items are resolved through TMDB. The tracker and TMDB fetchers take no context, so a client abort can't
// stop them.

var (
	notLinked    = failed(http.StatusConflict, ErrorCodeTrackerNotAuthorized, "no tracker account is linked")
	unsupported  = failed(http.StatusConflict, ErrorCodeTrackerUnsupported, "the linked tracker has no such list")
	noDeviceAuth = failed(http.StatusConflict, ErrorCodeTrackerUnsupported, "the selected tracker has no device-code login")
	noTokenStore = failed(http.StatusConflict, ErrorCodeTrackerUnsupported, "linking needs a JSON settings file")
	paidOnly     = failed(http.StatusConflict, ErrorCodeTrackerUnsupported, "the linked tracker offers this to paid accounts only")
	listMissing  = failed(http.StatusNotFound, ErrorCodeNotFound, "list not found")
	trackerDown  = failed(http.StatusBadGateway, ErrorCodeInternalError, "tracker request failed")

	// list ids go into tracker URL paths
	listIDRe = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,64}$`) // Simkl uses status:watching etc.

	// the activity buckets the Kodi tracker views refresh on
	viewBuckets = map[MediaKind]map[tracker.ListKind]tracker.Bucket{
		MediaKindMovies: {tracker.KindWatchlist: tracker.MoviesWatchlisted, tracker.KindCollection: tracker.MoviesCollected, tracker.KindList: tracker.ListsUpdated},
		MediaKindShows:  {tracker.KindWatchlist: tracker.ShowsWatchlisted, tracker.KindCollection: tracker.EpisodesCollected, tracker.KindList: tracker.ListsUpdated},
	}
)

// activeTracker returns the linked tracker. Provider.Authorized only checks stored credentials, it never starts a login.
func activeTracker() (tracker.Provider, *failure) {
	if p := tracker.Active(); p != nil {
		return p, nil
	}
	return nil, notLinked
}

// trackerFailure maps a tracker error; upstream messages are only logged.
func trackerFailure(p tracker.Provider, err error) *failure {
	switch {
	case errors.Is(err, tracker.ErrNotAuthorized), errors.Is(err, util.ErrUnauthorized):
		return notLinked
	case errors.Is(err, tracker.ErrUnsupported):
		return unsupported
	case errors.Is(err, tracker.ErrPaidAccount):
		return paidOnly
	case errors.Is(err, util.ErrNotFound):
		return listMissing
	}
	log.Warningf("%s request failed: %v", p.Name(), err)
	return trackerDown
}

// ref is a list entry's ids; the IMDB id is used only when the source has no TMDB id.
type ref struct {
	tmdb int
	imdb string
}

// tmdbID returns the TMDB id, looked up by IMDB id when the source gave none. 0 when unknown.
func tmdbID(r ref, movie bool) int {
	if r.tmdb != 0 || r.imdb == "" {
		return r.tmdb
	}
	return tmdb.FindID(r.imdb, movie)
}

// mediaItems resolves and fetches refs concurrently, in order, skipping those TMDB doesn't know.
func mediaItems(refs []ref, movies bool, lang string) []MediaItem {
	resolved := make([]int, len(refs))
	var wg sync.WaitGroup
	for n, r := range refs {
		wg.Go(func() { resolved[n] = tmdbID(r, movies) })
	}
	wg.Wait()

	ids := make([]int, 0, len(refs))
	for _, id := range resolved {
		if id != 0 {
			ids = append(ids, id)
		}
	}
	var items []MediaItem
	if movies {
		items = movieItems(tmdb.GetMovies(ids, lang))
	} else {
		items = showItems(tmdb.GetShows(ids, lang))
	}
	return annotate(items)
}

func (Server) ListLibraryMovies(c *gin.Context, p ListLibraryMoviesParams) {
	libraryPage(c, library.MovieType, p.Page, p.Limit, p.Lang)
}

func (Server) ListLibraryShows(c *gin.Context, p ListLibraryShowsParams) {
	libraryPage(c, library.ShowType, p.Page, p.Limit, p.Lang)
}

func libraryPage(c *gin.Context, mediaType int, page, limit *int, lang *string) {
	pg, lim, lng, f := args(page, limit, lang)
	if f != nil {
		fail(c, f)
		return
	}
	ids := library.ActiveIDs(mediaType)
	refs := []ref{}
	for _, id := range window(ids, pg, lim) {
		refs = append(refs, ref{tmdb: id})
	}
	c.JSON(http.StatusOK, listPage(mediaItems(refs, mediaType == library.MovieType, lng), len(ids), pg, lim))
}

func (Server) ListTraktChart(c *gin.Context, media MediaParam, list TraktChartName, p ListTraktChartParams) {
	page, limit, lang, f := args(p.Page, p.Limit, p.Lang, media, list)
	if f != nil {
		fail(c, f)
		return
	}
	// The chart names are Trakt's own; Trakt pages by limit, and its request cache key includes page and limit.
	movies := media == MediaKindMovies
	var refs []ref
	var total int
	var err error
	if movies {
		var ms []*trakt.Movies
		ms, total, err = trakt.TopMovies(string(list), strconv.Itoa(page), limit)
		for _, m := range ms {
			if m != nil && m.Movie != nil && m.Movie.IDs != nil {
				refs = append(refs, ref{m.Movie.IDs.TMDB, m.Movie.IDs.IMDB})
			}
		}
	} else {
		var ss []*trakt.Shows
		ss, total, err = trakt.TopShows(string(list), strconv.Itoa(page), limit)
		for _, s := range ss {
			if s != nil && s.Show != nil && s.Show.IDs != nil {
				refs = append(refs, ref{s.Show.IDs.TMDB, s.Show.IDs.IMDB})
			}
		}
	}
	if err != nil {
		log.Warningf("Trakt chart %s/%s failed: %v", media, list, err)
		fail(c, trackerDown)
		return
	}
	c.JSON(http.StatusOK, listPage(mediaItems(refs, movies, lang), total, page, limit))
}

// trackerList answers a page of a watchlist, collection or custom list of the linked tracker.
func trackerList(c *gin.Context, kind tracker.ListKind, media MediaKind, listID string, page, limit *int, lang *string) {
	pg, lim, lng, f := args(page, limit, lang, media)
	var p tracker.Provider
	if f == nil {
		p, f = activeTracker()
	}
	if f != nil {
		fail(c, f)
		return
	}
	t := tracker.MovieType
	if media == MediaKindShows {
		t = tracker.ShowType
	}
	_, collector := p.(tracker.Collector)
	_, reader := p.(tracker.ListReader)
	if (kind == tracker.KindCollection && !collector) || (kind == tracker.KindList && !reader) {
		fail(c, unsupported)
		return
	}
	items, err := tracker.ViewItems(p, kind, listID, t, viewBuckets[media][kind])
	if err != nil {
		fail(c, trackerFailure(p, err))
		return
	}
	refs := []ref{}
	for _, i := range window(items, pg, lim) {
		if i != nil {
			refs = append(refs, ref{i.IDs.TMDB, i.IDs.IMDB})
		}
	}
	c.JSON(http.StatusOK, listPage(mediaItems(refs, t == tracker.MovieType, lng), len(items), pg, lim))
}

func (Server) ListTrackerWatchlist(c *gin.Context, media MediaParam, p ListTrackerWatchlistParams) {
	trackerList(c, tracker.KindWatchlist, media, "", p.Page, p.Limit, p.Lang)
}

func (Server) ListTrackerCollection(c *gin.Context, media MediaParam, p ListTrackerCollectionParams) {
	trackerList(c, tracker.KindCollection, media, "", p.Page, p.Limit, p.Lang)
}

func (Server) ListTrackerListItems(c *gin.Context, listID string, p ListTrackerListItemsParams) {
	if !listIDRe.MatchString(listID) {
		fail(c, bad("invalid list_id"))
		return
	}
	trackerList(c, tracker.KindList, p.Media, listID, p.Page, p.Limit, p.Lang)
}

func (Server) ListTrackerLists(c *gin.Context) {
	p, f := activeTracker()
	if f != nil {
		fail(c, f)
		return
	}
	l, ok := p.(tracker.ListReader)
	if !ok {
		fail(c, unsupported)
		return
	}
	lists, err := l.Lists()
	if err != nil {
		fail(c, trackerFailure(p, err))
		return
	}
	out := []TrackerList{}
	for _, list := range lists {
		if list != nil {
			out = append(out, TrackerList{Id: list.ID, Name: list.Name, ItemCount: list.ItemCount})
		}
	}
	c.JSON(http.StatusOK, out)
}

func (Server) ListTrackerProgress(c *gin.Context, params ListTrackerProgressParams) {
	pg, lim, lng, f := args(params.Page, params.Limit, params.Lang)
	var p tracker.Provider
	if f == nil {
		p, f = activeTracker()
	}
	if f != nil {
		fail(c, f)
		return
	}
	pr, ok := p.(tracker.ProgressReader)
	if !ok {
		fail(c, unsupported)
		return
	}
	// as the Kodi progress view does
	activities, err := tracker.GetActivities(p, "view.progress")
	items, err := pr.Progress(err != nil || activities.Changed(tracker.EpisodesWatched))
	if err != nil {
		fail(c, trackerFailure(p, err))
		return
	}

	page := window(items, pg, lim)
	found := make([]*NextEpisode, len(page))
	var wg sync.WaitGroup
	for n, i := range page {
		if i == nil {
			continue
		}
		wg.Go(func() {
			id := tmdbID(ref{i.IDs.TMDB, i.IDs.IMDB}, false)
			if id == 0 {
				return
			}
			if s := tmdb.GetShow(id, lng); s != nil && s.ID != 0 {
				ne := nextEpisode(s, tmdb.GetEpisode(id, i.Season, i.Episode, lng), i.Season, i.Episode)
				found[n] = &ne
			}
		})
	}
	wg.Wait()

	out := NextEpisodePage{Items: []NextEpisode{}, Page: pg, Limit: lim}
	out.TotalPages, out.TotalResults = pageTotals(len(items), pg, lim)
	for _, ne := range found {
		if ne != nil {
			out.Items = append(out.Items, *ne)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (Server) StartTrackerAuth(c *gin.Context) {
	if !config.SettingsWritable() { // the token could not be saved
		fail(c, noTokenStore)
		return
	}
	p := tracker.Selected() // an unlinked tracker is the one to link
	s, ok := p.(tracker.DeviceAuthStarter)
	if !ok {
		fail(c, noDeviceAuth)
		return
	}
	code, err := s.StartAuth()
	if err != nil {
		log.Warningf("%s authorization failed: %v", p.Name(), err)
		fail(c, trackerDown)
		return
	}
	c.JSON(http.StatusOK, TrackerAuthCode{
		Tracker:         *trackerInfo(p),
		UserCode:        code.UserCode,
		VerificationUrl: code.VerificationURL,
		ExpiresIn:       code.ExpiresIn,
		Interval:        code.Interval,
	})
}
