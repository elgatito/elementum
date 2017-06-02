package providers

import (
	"github.com/scakemyer/quasar/bittorrent"
	"github.com/scakemyer/quasar/tmdb"
)

type Searcher interface {
	SearchLinks(query string) []*bittorrent.TorrentFile
}

type MovieSearcher interface {
	SearchMovieLinks(movie *tmdb.Movie) []*bittorrent.TorrentFile
}

type SeasonSearcher interface {
	SearchSeasonLinks(show *tmdb.Show, season *tmdb.Season) []*bittorrent.TorrentFile
}

type EpisodeSearcher interface {
	SearchEpisodeLinks(show *tmdb.Show, episode *tmdb.Episode) []*bittorrent.TorrentFile
}
