package apiv1

import (
	"context"
	"net/http"
)

// Operations not implemented yet; later steps replace these one by one.

var notImplemented = newError(ErrorCodeNotImplemented, "not implemented")

func (Server) CreatePlayback(context.Context, CreatePlaybackRequestObject) (CreatePlaybackResponseObject, error) {
	return CreatePlaybackdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) DeletePlayback(context.Context, DeletePlaybackRequestObject) (DeletePlaybackResponseObject, error) {
	return DeletePlaybackdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) GetMovie(context.Context, GetMovieRequestObject) (GetMovieResponseObject, error) {
	return GetMoviedefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) GetPlayback(context.Context, GetPlaybackRequestObject) (GetPlaybackResponseObject, error) {
	return GetPlaybackdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) GetSeason(context.Context, GetSeasonRequestObject) (GetSeasonResponseObject, error) {
	return GetSeasondefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) GetShow(context.Context, GetShowRequestObject) (GetShowResponseObject, error) {
	return GetShowdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) GetStream(context.Context, GetStreamRequestObject) (GetStreamResponseObject, error) {
	return GetStreamdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListEpisodeStreams(context.Context, ListEpisodeStreamsRequestObject) (ListEpisodeStreamsResponseObject, error) {
	return ListEpisodeStreamsdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListLibraryMovies(context.Context, ListLibraryMoviesRequestObject) (ListLibraryMoviesResponseObject, error) {
	return ListLibraryMoviesdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListLibraryShows(context.Context, ListLibraryShowsRequestObject) (ListLibraryShowsResponseObject, error) {
	return ListLibraryShowsdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListMovieGenres(context.Context, ListMovieGenresRequestObject) (ListMovieGenresResponseObject, error) {
	return ListMovieGenresdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListMovieStreams(context.Context, ListMovieStreamsRequestObject) (ListMovieStreamsResponseObject, error) {
	return ListMovieStreamsdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListMovies(context.Context, ListMoviesRequestObject) (ListMoviesResponseObject, error) {
	return ListMoviesdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListShowGenres(context.Context, ListShowGenresRequestObject) (ListShowGenresResponseObject, error) {
	return ListShowGenresdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListShows(context.Context, ListShowsRequestObject) (ListShowsResponseObject, error) {
	return ListShowsdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListTrackerCollection(context.Context, ListTrackerCollectionRequestObject) (ListTrackerCollectionResponseObject, error) {
	return ListTrackerCollectiondefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListTrackerListItems(context.Context, ListTrackerListItemsRequestObject) (ListTrackerListItemsResponseObject, error) {
	return ListTrackerListItemsdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListTrackerLists(context.Context, ListTrackerListsRequestObject) (ListTrackerListsResponseObject, error) {
	return ListTrackerListsdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListTrackerProgress(context.Context, ListTrackerProgressRequestObject) (ListTrackerProgressResponseObject, error) {
	return ListTrackerProgressdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListTrackerWatchlist(context.Context, ListTrackerWatchlistRequestObject) (ListTrackerWatchlistResponseObject, error) {
	return ListTrackerWatchlistdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListTraktChart(context.Context, ListTraktChartRequestObject) (ListTraktChartResponseObject, error) {
	return ListTraktChartdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ReportPlaybackProgress(context.Context, ReportPlaybackProgressRequestObject) (ReportPlaybackProgressResponseObject, error) {
	return ReportPlaybackProgressdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) Search(context.Context, SearchRequestObject) (SearchResponseObject, error) {
	return SearchdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}
