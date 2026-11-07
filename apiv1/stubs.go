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

func (Server) GetPlayback(context.Context, GetPlaybackRequestObject) (GetPlaybackResponseObject, error) {
	return GetPlaybackdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) GetStream(context.Context, GetStreamRequestObject) (GetStreamResponseObject, error) {
	return GetStreamdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListLibraryMovies(context.Context, ListLibraryMoviesRequestObject) (ListLibraryMoviesResponseObject, error) {
	return ListLibraryMoviesdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
}

func (Server) ListLibraryShows(context.Context, ListLibraryShowsRequestObject) (ListLibraryShowsResponseObject, error) {
	return ListLibraryShowsdefaultJSONResponse{Body: notImplemented, StatusCode: http.StatusNotImplemented}, nil
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
