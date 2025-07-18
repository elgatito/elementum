package providers

import (
	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/tmdb"
)

// Searcher ...
type Searcher interface {
	SearchLinks(query string) []*bittorrent.TorrentFile
}

// MovieSearcher ...
type MovieSearcher interface {
	SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile
	SearchMovieLinksSilent(movie *tmdb.Movie, withAuth bool) []*bittorrent.TorrentFile
}

// SeasonSearcher ...
type SeasonSearcher interface {
	SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile
}

// EpisodeSearcher ...
type EpisodeSearcher interface {
	SearchEpisodeLinks(show *tmdb.Show, season *tmdb.Season, episode *tmdb.Episode) []*bittorrent.TorrentFile
}
