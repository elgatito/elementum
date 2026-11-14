package jsonapi

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/database"
)

// Torrents go through the same Service and Torrent calls as the Kodi /torrents routes (api/torrents.go),
// minus the Kodi list items and dialogs.

var (
	hashRe        = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	badHash       = bad("info_hash must be 40 hex characters")
	torrentAbsent = failed(http.StatusNotFound, ErrorCodeNotFound, "torrent not found")

	// Indexed by bittorrent.Status*.
	torrentStates = []TorrentState{
		TorrentStateQueued, TorrentStateChecking, TorrentStateFinding, TorrentStateDownloading, TorrentStateFinished,
		TorrentStateSeeding, TorrentStateAllocating, TorrentStateStalled, TorrentStatePaused, TorrentStateBuffering, TorrentStatePlaying,
	}
)

// torrentInfo is what the DTO needs from a running torrent, so the mapping can be tested without libtorrent.
type torrentInfo struct {
	hash, name             string
	size                   int64
	percent                float64 // 0-100, as Torrent.GetProgress
	state                  int     // bittorrent.Status*
	paused                 bool
	down, up, seeds, peers int
	added                  time.Time
	db                     *database.BTItem
	players                int32
}

func toTorrent(i torrentInfo) Torrent {
	t := Torrent{
		InfoHash: strings.ToLower(i.hash), Name: i.name, SizeBytes: i.size,
		Progress: min(max(i.percent/100, 0), 1), DownloadRate: i.down, UploadRate: i.up, Seeds: i.seeds, Peers: i.peers,
		State: TorrentStateQueued, Paused: i.paused, AddedAt: i.added.Unix(), HasPlayer: i.players > 0,
	}
	if i.state >= 0 && i.state < len(torrentStates) {
		t.State = torrentStates[i.state]
	}
	if t.Name == "" { // a magnet without metadata
		t.Name = t.InfoHash
	}
	if d := i.db; d != nil {
		switch {
		case d.Type == "movie" && d.ID > 0:
			t.TmdbId, t.MediaType = ptr(d.ID), ptr(TorrentMediaTypeMovie)
		case (d.Type == "show" || d.Type == "episode") && d.ShowID > 0: // the show's id: the torrent is for one of its episodes
			t.TmdbId, t.MediaType, t.Season, t.Episode = ptr(d.ShowID), ptr(TorrentMediaTypeEpisode), ptr(d.Season), positive(d.Episode)
		}
	}
	return t
}

func (Server) ListTorrents(c *gin.Context) {
	torrents := sessions.bts.GetTorrents()
	out := make([]Torrent, 0, len(torrents))
	for _, t := range torrents {
		if t == nil || t.Closer.IsSet() || sessions.bts.Closer.IsSet() {
			continue
		}
		st, state := t.GetStatus(), t.GetSmartState()
		out = append(out, toTorrent(torrentInfo{
			hash: t.InfoHash(), name: t.Name(), size: t.Length(), percent: t.GetProgress(), state: state,
			// state is paused for the Kodi-wide "pause all" too, so the flag follows it
			paused: st.Paused || state == bittorrent.StatusPaused, down: st.DownloadRate, up: st.UploadRate, seeds: st.NumSeeds, peers: st.NumPeers,
			added: t.GetAddedTime(), db: t.GetDBItem(), players: t.PlayerAttached.Load(),
		}))
	}
	c.JSON(http.StatusOK, out)
}

// findTorrent validates the hash and looks the torrent up; exactly one of the results is set.
func findTorrent(hash string) (*bittorrent.Torrent, *failure) {
	if !hashRe.MatchString(hash) {
		return nil, badHash
	}
	if t := sessions.bts.GetTorrentByHash(strings.ToLower(hash)); t != nil {
		return t, nil
	}
	return nil, torrentAbsent
}

// torrentAction runs do on the torrent and answers 204, or the lookup's failure.
func torrentAction(c *gin.Context, hash string, do func(*bittorrent.Torrent)) {
	t, f := findTorrent(hash)
	if f != nil {
		fail(c, f)
		return
	}
	do(t)
	c.Status(http.StatusNoContent)
}

func (Server) PauseTorrent(c *gin.Context, hash InfoHashParam) {
	torrentAction(c, hash, (*bittorrent.Torrent).Pause)
}

func (Server) ResumeTorrent(c *gin.Context, hash InfoHashParam) {
	torrentAction(c, hash, func(t *bittorrent.Torrent) {
		if sessions.bts.IsPaused() { // Kodi's "pause all" would keep the torrent paused
			sessions.bts.Resume()
		}
		t.Resume()
	})
}

// RemoveTorrent drops the torrent with no Kodi host, so nothing is asked: the data goes only with delete_files.
// A playback session on the torrent is stopped after the removal (not before: the player's own Close would
// remove the torrent first, by the keep-files settings, and ignore delete_files).
func (Server) RemoveTorrent(c *gin.Context, hash InfoHashParam, p RemoveTorrentParams) {
	torrentAction(c, hash, func(t *bittorrent.Torrent) {
		deleteFiles := p.DeleteFiles != nil && *p.DeleteFiles
		sessions.bts.RemoveTorrent(nil, t, bittorrent.RemoveOptions{ForceDrop: true, ForceDelete: deleteFiles, ForceKeepTorrentData: !deleteFiles})

		stopSessions(func(sess *session) bool {
			return sess.hash == t.InfoHash() || (sess.state != PlaybackStateResolving && sess.player != nil && sess.player.GetTorrent() == t)
		})
	})
}

// stopSessions stops and forgets the sessions for which uses is true.
func stopSessions(uses func(*session) bool) {
	sessions.Lock()
	defer sessions.Unlock()
	for id, sess := range sessions.byID {
		if uses(sess) {
			sess.stop()
			delete(sessions.byID, id)
		}
	}
}
