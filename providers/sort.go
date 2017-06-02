package providers

import (
	"math"
	"sort"

	"github.com/scakemyer/quasar/config"
	"github.com/scakemyer/quasar/bittorrent"
)

type BySeeds []*bittorrent.TorrentFile
func (a BySeeds) Len() int           { return len(a) }
func (a BySeeds) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a BySeeds) Less(i, j int) bool { return a[i].Seeds < a[j].Seeds }

type ByResolution []*bittorrent.TorrentFile
func (a ByResolution) Len() int           { return len(a) }
func (a ByResolution) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByResolution) Less(i, j int) bool { return a[i].Resolution < a[j].Resolution }

type ByQuality []*bittorrent.TorrentFile
func (a ByQuality) Len() int           { return len(a) }
func (a ByQuality) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByQuality) Less(i, j int) bool { return QualityFactor(a[i]) < QualityFactor(a[j]) }


type lessFunc func(p1, p2 *bittorrent.TorrentFile) bool
type multiSorter struct {
	torrents []*bittorrent.TorrentFile
	less     []lessFunc
}

func (ms *multiSorter) Len() int      { return len(ms.torrents) }
func (ms *multiSorter) Swap(i, j int) { ms.torrents[i], ms.torrents[j] = ms.torrents[j], ms.torrents[i] }
func (ms *multiSorter) Less(i, j int) bool {
	p, q := ms.torrents[i], ms.torrents[j]
	var k int
	for k = 0; k < len(ms.less) - 1; k++ {
		less := ms.less[k]
		switch {
		case less(p, q):
			return true
		case less(q, p):
			return false
		}
	}
	return ms.less[k](p, q)
}

func (ms *multiSorter) Sort(torrents []*bittorrent.TorrentFile) {
	ms.torrents = torrents
	sort.Sort(ms)
}

func SortBy(less ...lessFunc) *multiSorter {
	return &multiSorter{
		less: less,
	}
}


func Balanced(t *bittorrent.TorrentFile) float64 {
	result := float64(t.Seeds) + (float64(t.Seeds) * float64(config.Get().PercentageAdditionalSeeders) / 100)
	return result
}

func Resolution720p1080p(t *bittorrent.TorrentFile) int {
	result := t.Resolution
	if t.Resolution == bittorrent.Resolution720p {
		result = -1
	} else if t.Resolution == bittorrent.Resolution1080p {
		result = 0
	}
	return result
}

func Resolution720p480p(t *bittorrent.TorrentFile) int {
	result := t.Resolution
	if t.Resolution == bittorrent.Resolution720p {
		result = -1
	}
	return result
}

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
