package bittorrent

import (
	"maps"
	"testing"

	"github.com/elgatito/elementum/config"
)

func TestCompletedFileNames(t *testing.T) {
	cases := []struct {
		files   []string
		isMovie bool
		season  int
		episode int
		want    map[string]string
	}{
		{
			files:   []string{"Dir/movie.2020.1080p.mkv", "Dir/movie.2020.1080p.eng.srt", "Dir/Sample/sample.mkv", "Dir/info.txt"},
			isMovie: true,
			want:    map[string]string{"Dir/movie.2020.1080p.mkv": "N.mkv", "Dir/movie.2020.1080p.eng.srt": "N.eng.srt"},
		},
		{
			files:   []string{"Dir/cd1.avi", "Dir/cd2.avi"},
			isMovie: true,
			want:    map[string]string{},
		},
		{
			files:  []string{"Pack/show.s01e01.mkv", "Pack/show.S01.E02.mkv", "Pack/show 1x03.mp4", "Pack/extras.mkv", "Pack/1920x1080.mkv"},
			season: 1,
			want:   map[string]string{"Pack/show.s01e01.mkv": "N S01E01.mkv", "Pack/show.S01.E02.mkv": "N S01E02.mkv", "Pack/show 1x03.mp4": "N S01E03.mp4"},
		},
		{
			files:   []string{"episode.mkv"},
			season:  2,
			episode: 5,
			want:    map[string]string{"episode.mkv": "N S02E05.mkv"},
		},
		{
			files: []string{"a.S01E01.720p.mkv", "a.S01E01.1080p.mkv"},
			want:  map[string]string{},
		},
	}

	for i, c := range cases {
		if got := completedFileNames(c.files, "N", c.isMovie, c.season, c.episode); !maps.Equal(got, c.want) {
			t.Errorf("case %d: got %v, want %v", i, got, c.want)
		}
	}
}

func TestSeedLimitReached(t *testing.T) {
	disk := &Torrent{DownloadStorage: config.StorageFile}
	cases := []struct {
		cfg  config.Configuration
		tor  *Torrent
		ts   TorrentStatus
		want string
	}{
		{cfg: config.Configuration{SeedTimeLimit: 60}, tor: disk, ts: TorrentStatus{SeedingTime: 60}, want: "Seeding time limit reached"},
		{cfg: config.Configuration{SeedTimeLimit: 60}, tor: disk, ts: TorrentStatus{FinishedTime: 60}, want: "Seeding time limit reached"},
		{cfg: config.Configuration{SeedTimeLimit: 60}, tor: disk, ts: TorrentStatus{SeedingTime: 59}},
		{cfg: config.Configuration{SeedTimeLimit: 60, SeedForever: true}, tor: disk, ts: TorrentStatus{SeedingTime: 60}},
		{cfg: config.Configuration{SeedTimeLimit: 60}, tor: &Torrent{DownloadStorage: config.StorageMemory}, ts: TorrentStatus{SeedingTime: 60}},
		{cfg: config.Configuration{SeedTimeRatioLimit: 100}, tor: disk, ts: TorrentStatus{SeedingTime: 50, ActiveTime: 100}, want: "Seeding time ratio reached"},
		{cfg: config.Configuration{SeedTimeRatioLimit: 100}, tor: disk, ts: TorrentStatus{SeedingTime: 40, ActiveTime: 100}},
		{cfg: config.Configuration{ShareRatioLimit: 200}, tor: disk, ts: TorrentStatus{AllTimeDownload: 10, AllTimeUpload: 20}, want: "Share ratio reached"},
		{cfg: config.Configuration{ShareRatioLimit: 200}, tor: disk, ts: TorrentStatus{AllTimeDownload: 10, AllTimeUpload: 19}},
	}

	for i, c := range cases {
		s := &Service{config: &c.cfg}
		if got := s.seedLimitReached(c.tor, &c.ts, 100); got != c.want {
			t.Errorf("case %d: got %q, want %q", i, got, c.want)
		}
	}
}
