package providers

import (
	"math"
	"slices"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
)

type lessFunc func(p1, p2 *bittorrent.TorrentFile) bool

// MultiSorter ...
type MultiSorter struct {
	less []lessFunc
}

// Sort ...
func (ms *MultiSorter) Sort(torrents []*bittorrent.TorrentFile) {
	slices.SortFunc(torrents, func(p, q *bittorrent.TorrentFile) int {
		for _, less := range ms.less {
			switch {
			case less(p, q):
				return -1
			case less(q, p):
				return 1
			}
		}
		return 0
	})
}

// SortBy ...
func SortBy(less ...lessFunc) *MultiSorter {
	return &MultiSorter{
		less: less,
	}
}

// Balanced ...
func Balanced(t *bittorrent.TorrentFile) float64 {
	result := float64(t.Seeds) + (float64(t.Seeds) * float64(config.Get().PercentageAdditionalSeeders) / 100)
	return result
}

// Resolution720p1080p ...
func Resolution720p1080p(t *bittorrent.TorrentFile) int {
	result := t.Resolution
	if t.Resolution == bittorrent.Resolution720p {
		result = -1
	} else if t.Resolution == bittorrent.Resolution1080p {
		result = 0
	}
	return result
}

// Resolution720p480p ...
func Resolution720p480p(t *bittorrent.TorrentFile) int {
	result := t.Resolution
	if t.Resolution == bittorrent.Resolution720p {
		result = -1
	}
	return result
}

// QualityFactor ...
func QualityFactor(t *bittorrent.TorrentFile) float64 {
	result := float64(t.Seeds)
	if t.Resolution > bittorrent.ResolutionUnknown {
		result *= math.Pow(float64(t.Resolution), 3)
	}
	if t.RipType > bittorrent.RipUnknown {
		result *= float64(t.RipType)
	}
	return result
}
