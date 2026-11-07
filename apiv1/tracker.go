package apiv1

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"sync"

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
	notLinked   = failure{http.StatusConflict, newError(ErrorCodeTrackerNotAuthorized, "no tracker account is linked")}
	unsupported = failure{http.StatusConflict, newError(ErrorCodeTrackerUnsupported, "the linked tracker has no such list")}
	paidOnly    = failure{http.StatusConflict, newError(ErrorCodeTrackerUnsupported, "the linked tracker offers this to paid accounts only")}
	listMissing = failure{http.StatusNotFound, newError(ErrorCodeNotFound, "list not found")}
	trackerDown = failure{http.StatusBadGateway, newError(ErrorCodeInternalError, "tracker request failed")}
	badListID   = failure{http.StatusBadRequest, newError(ErrorCodeBadRequest, "invalid list_id")}

	// list ids go into tracker URL paths
	listIDRe = regexp.MustCompile(`^[A-Za-z0-9_:-]{1,64}$`) // Simkl uses status:watching etc.

	// the activity buckets the Kodi tracker views refresh on
	viewBuckets = map[MediaKind]map[tracker.ListKind]tracker.Bucket{
		MediaKindMovies: {tracker.KindWatchlist: tracker.MoviesWatchlisted, tracker.KindCollection: tracker.MoviesCollected, tracker.KindList: tracker.ListsUpdated},
		MediaKindShows:  {tracker.KindWatchlist: tracker.ShowsWatchlisted, tracker.KindCollection: tracker.EpisodesCollected, tracker.KindList: tracker.ListsUpdated},
	}
)

// failure is an error response, for the operations' default response types.
type failure struct {
	status int
	body   Error
}

func badParams(e *Error) *failure { return &failure{http.StatusBadRequest, *e} }

// activeTracker returns the linked tracker. Provider.Authorized only checks stored credentials, it never starts a login.
func activeTracker() (tracker.Provider, *failure) {
	if p := tracker.Active(); p != nil {
		return p, nil
	}
	return nil, &notLinked
}

// trackerFailure maps a tracker error; upstream messages are only logged.
func trackerFailure(p tracker.Provider, err error) *failure {
	switch {
	case errors.Is(err, tracker.ErrNotAuthorized):
		return &notLinked
	case errors.Is(err, tracker.ErrUnsupported):
		return &unsupported
	case errors.Is(err, tracker.ErrPaidAccount):
		return &paidOnly
	case errors.Is(err, util.ErrNotFound):
		return &listMissing
	}
	log.Warningf("%s request failed: %v", p.Name(), err)
	return &trackerDown
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
	if res := tmdb.Find(r.imdb, "imdb_id"); res != nil {
		found := res.TVResults
		if movie {
			found = res.MovieResults
		}
		if len(found) > 0 && found[0] != nil {
			return found[0].ID
		}
	}
	return 0
}

// mediaItems resolves and fetches refs concurrently, in order, skipping those TMDB doesn't know.
func mediaItems(refs []ref, movies bool, lang string) []MediaItem {
	found := make([]*MediaItem, len(refs))
	var wg sync.WaitGroup
	for n, r := range refs {
		wg.Go(func() {
			id := tmdbID(r, movies)
			if id == 0 {
				return
			}
			var item MediaItem
			if movies {
				m := tmdb.GetMovie(id, lang)
				if m == nil || m.ID == 0 {
					return
				}
				item = movieItem(m)
			} else {
				s := tmdb.GetShow(id, lang)
				if s == nil || s.ID == 0 {
					return
				}
				item = showItem(s)
			}
			found[n] = &item
		})
	}
	wg.Wait()

	items := []MediaItem{}
	for _, i := range found {
		if i != nil {
			items = append(items, *i)
		}
	}
	return items
}

func (Server) ListLibraryMovies(_ context.Context, req ListLibraryMoviesRequestObject) (ListLibraryMoviesResponseObject, error) {
	p := req.Params
	page, f := libraryPage(library.MovieType, p.Page, p.Limit, p.Lang)
	if f != nil {
		return ListLibraryMoviesdefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	return ListLibraryMovies200JSONResponse{MediaItemPageJSONResponse(page)}, nil
}

func (Server) ListLibraryShows(_ context.Context, req ListLibraryShowsRequestObject) (ListLibraryShowsResponseObject, error) {
	p := req.Params
	page, f := libraryPage(library.ShowType, p.Page, p.Limit, p.Lang)
	if f != nil {
		return ListLibraryShowsdefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	return ListLibraryShows200JSONResponse{MediaItemPageJSONResponse(page)}, nil
}

func libraryPage(mediaType int, page, limit *int, lang *string) (MediaItemPage, *failure) {
	pg, lim, lng, e := args(page, limit, lang)
	if e != nil {
		return MediaItemPage{}, badParams(e)
	}
	ids := library.ActiveIDs(mediaType)
	refs := []ref{}
	for _, id := range window(ids, pg, lim) {
		refs = append(refs, ref{tmdb: id})
	}
	return listPage(mediaItems(refs, mediaType == library.MovieType, lng), len(ids), pg, lim), nil
}

func (Server) ListTraktChart(_ context.Context, req ListTraktChartRequestObject) (ListTraktChartResponseObject, error) {
	p := req.Params
	page, limit, lang, e := args(p.Page, p.Limit, p.Lang, req.Media, req.List)
	if e != nil {
		return ListTraktChartdefaultJSONResponse{Body: *e, StatusCode: http.StatusBadRequest}, nil
	}
	// The chart names are Trakt's own; Trakt pages by limit, and its request cache key includes page and limit.
	movies := req.Media == MediaKindMovies
	var refs []ref
	var total int
	var err error
	if movies {
		var ms []*trakt.Movies
		ms, total, err = trakt.TopMovies(string(req.List), strconv.Itoa(page), limit)
		for _, m := range ms {
			if m != nil && m.Movie != nil && m.Movie.IDs != nil {
				refs = append(refs, ref{m.Movie.IDs.TMDB, m.Movie.IDs.IMDB})
			}
		}
	} else {
		var ss []*trakt.Shows
		ss, total, err = trakt.TopShows(string(req.List), strconv.Itoa(page), limit)
		for _, s := range ss {
			if s != nil && s.Show != nil && s.Show.IDs != nil {
				refs = append(refs, ref{s.Show.IDs.TMDB, s.Show.IDs.IMDB})
			}
		}
	}
	if err != nil {
		log.Warningf("Trakt chart %s/%s failed: %v", req.Media, req.List, err)
		return ListTraktChartdefaultJSONResponse{Body: trackerDown.body, StatusCode: trackerDown.status}, nil
	}
	return ListTraktChart200JSONResponse{MediaItemPageJSONResponse(listPage(mediaItems(refs, movies, lang), total, page, limit))}, nil
}

// trackerList pages a watchlist, collection or custom list of the linked tracker.
func trackerList(kind tracker.ListKind, media MediaKind, listID string, page, limit *int, lang *string) (MediaItemPage, *failure) {
	pg, lim, lng, e := args(page, limit, lang, media)
	if e != nil {
		return MediaItemPage{}, badParams(e)
	}
	p, f := activeTracker()
	if f != nil {
		return MediaItemPage{}, f
	}
	t := tracker.MovieType
	if media == MediaKindShows {
		t = tracker.ShowType
	}
	if _, ok := p.(tracker.Collector); kind == tracker.KindCollection && !ok {
		return MediaItemPage{}, &unsupported
	}
	if _, ok := p.(tracker.ListReader); kind == tracker.KindList && !ok {
		return MediaItemPage{}, &unsupported
	}
	items, err := tracker.ViewItems(p, kind, listID, t, viewBuckets[media][kind])
	if err != nil {
		return MediaItemPage{}, trackerFailure(p, err)
	}
	refs := []ref{}
	for _, i := range window(items, pg, lim) {
		if i != nil {
			refs = append(refs, ref{i.IDs.TMDB, i.IDs.IMDB})
		}
	}
	return listPage(mediaItems(refs, t == tracker.MovieType, lng), len(items), pg, lim), nil
}

func (Server) ListTrackerWatchlist(_ context.Context, req ListTrackerWatchlistRequestObject) (ListTrackerWatchlistResponseObject, error) {
	p := req.Params
	page, f := trackerList(tracker.KindWatchlist, req.Media, "", p.Page, p.Limit, p.Lang)
	if f != nil {
		return ListTrackerWatchlistdefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	return ListTrackerWatchlist200JSONResponse{MediaItemPageJSONResponse(page)}, nil
}

func (Server) ListTrackerCollection(_ context.Context, req ListTrackerCollectionRequestObject) (ListTrackerCollectionResponseObject, error) {
	p := req.Params
	page, f := trackerList(tracker.KindCollection, req.Media, "", p.Page, p.Limit, p.Lang)
	if f != nil {
		return ListTrackerCollectiondefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	return ListTrackerCollection200JSONResponse{MediaItemPageJSONResponse(page)}, nil
}

func (Server) ListTrackerListItems(_ context.Context, req ListTrackerListItemsRequestObject) (ListTrackerListItemsResponseObject, error) {
	p := req.Params
	if !listIDRe.MatchString(req.ListId) {
		return ListTrackerListItemsdefaultJSONResponse{Body: badListID.body, StatusCode: badListID.status}, nil
	}
	page, f := trackerList(tracker.KindList, p.Media, req.ListId, p.Page, p.Limit, p.Lang)
	if f != nil {
		return ListTrackerListItemsdefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	return ListTrackerListItems200JSONResponse{MediaItemPageJSONResponse(page)}, nil
}

func (Server) ListTrackerLists(context.Context, ListTrackerListsRequestObject) (ListTrackerListsResponseObject, error) {
	p, f := activeTracker()
	if f != nil {
		return ListTrackerListsdefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	l, ok := p.(tracker.ListReader)
	if !ok {
		return ListTrackerListsdefaultJSONResponse{Body: unsupported.body, StatusCode: unsupported.status}, nil
	}
	lists, err := l.Lists()
	if err != nil {
		f = trackerFailure(p, err)
		return ListTrackerListsdefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	out := ListTrackerLists200JSONResponse{}
	for _, list := range lists {
		if list != nil {
			out = append(out, TrackerList{Id: list.ID, Name: list.Name, ItemCount: list.ItemCount})
		}
	}
	return out, nil
}

func (Server) ListTrackerProgress(_ context.Context, req ListTrackerProgressRequestObject) (ListTrackerProgressResponseObject, error) {
	fail := func(f *failure) (ListTrackerProgressResponseObject, error) {
		return ListTrackerProgressdefaultJSONResponse{Body: f.body, StatusCode: f.status}, nil
	}
	pg, lim, lng, e := args(req.Params.Page, req.Params.Limit, req.Params.Lang)
	if e != nil {
		return fail(badParams(e))
	}
	p, f := activeTracker()
	if f != nil {
		return fail(f)
	}
	pr, ok := p.(tracker.ProgressReader)
	if !ok {
		return fail(&unsupported)
	}
	// as the Kodi progress view does
	activities, err := tracker.GetActivities(p, "view.progress")
	items, err := pr.Progress(err != nil || activities.Changed(tracker.EpisodesWatched))
	if err != nil {
		return fail(trackerFailure(p, err))
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
	return ListTrackerProgress200JSONResponse(out), nil
}
