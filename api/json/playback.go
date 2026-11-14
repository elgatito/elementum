package jsonapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/tmdb"
)

// Playback sessions drive a bittorrent.Player in External mode: no Kodi dialogs or player callbacks.
// One session is active at a time, and its file is served by getStream behind an HMAC-signed link.

const (
	idleTimeout = 60 * time.Second
	linkTTL     = 12 * time.Hour
)

var (
	// linkKey signs stream links. It lives as long as the process, like the sessions it signs for.
	linkKey = func() []byte { k := make([]byte, 32); rand.Read(k); return k }()

	forbidden       = failed(http.StatusForbidden, ErrorCodeForbidden, "forbidden")
	sessionNotFound = failed(http.StatusNotFound, ErrorCodeNotFound, "session not found")
)

type session struct {
	id     string
	player *bittorrent.Player
	hash   string // info hash, to find the torrent while the player still adds it; "" when unknown
	state  PlaybackState
	files  []PlaybackFile   // set in choosing_file
	file   *bittorrent.File // set in ready
	resume *float64         // set in ready
	seen   time.Time        // last status or progress call
	stall  stall            // buffering progress watch
}

// stall detects a buffering torrent that makes no progress: Buffer itself has no timeout once the
// metadata is in, so a torrent without peers would stay in buffering forever.
type stall struct {
	progress float64
	since    time.Time
}

// update records the latest progress and reports whether none was made for timeout.
func (s *stall) update(progress float64, rate int, now time.Time, timeout time.Duration) bool {
	if s.since.IsZero() || progress > s.progress || rate > 0 {
		s.progress, s.since = progress, now
	}
	return now.Sub(s.since) > timeout
}

var (
	sessions = struct {
		sync.Mutex
		bts    *bittorrent.Service
		byID   map[string]*session
		active *session
	}{byID: map[string]*session{}}
	reaper sync.Once
)

// CreatePlayback validates the uri, stops the active session and starts buffering in the background.
func (Server) CreatePlayback(c *gin.Context) {
	b := body[PlaybackRequest](c)
	if b == nil {
		return
	}
	f := checkParams(nil, nil, b.Type)
	switch {
	case f != nil:
	case b.TmdbId < 1 || (b.FileIndex != nil && *b.FileIndex < 0):
		f = bad("tmdb_id must be 1 or more, file_index 0 or more")
	case b.Type == PlaybackTypeEpisode && (b.Season == nil || b.Episode == nil || *b.Season < 0 || *b.Episode < 1):
		f = bad("season (0 or more) and episode (1 or more) are required for an episode")
	}
	if f != nil {
		fail(c, f)
		return
	}

	// Same params as the Kodi /play route; for an episode TMDBId is the episode's id and ShowID the show's.
	params := bittorrent.PlayerParams{
		External: true, ContentType: string(b.Type), TMDBId: b.TmdbId,
		OriginalIndex: -1, FileIndex: -1, NextOriginalIndex: -1, NextFileIndex: -1, MinSize: -1, KodiPosition: -1,
	}
	cacheKey := strconv.Itoa(b.TmdbId)
	if b.Type == PlaybackTypeEpisode {
		cacheKey = providers.EpisodeCacheKey(b.TmdbId, *b.Season, *b.Episode)
		params.ShowID, params.Season, params.Episode = b.TmdbId, *b.Season, *b.Episode
	}
	if b.FileIndex != nil {
		params.OriginalIndex = *b.FileIndex
	}

	cached, _ := providers.GetCachedTorrents(cacheKey)
	uri, hash, ok := playableURI(b.Uri, cached, config.Get().TemporaryPath)
	if !ok {
		fail(c, bad("uri is not a playable stream"))
		return
	}
	params.URI = uri

	if b.Type == PlaybackTypeEpisode {
		params.TMDBId = 0 // no scrobbling without the episode id
		if e := tmdb.GetEpisode(b.TmdbId, *b.Season, *b.Episode, config.Get().Language); e != nil {
			params.TMDBId = e.ID
		}
	}

	player := bittorrent.NewPlayer(sessions.bts, params, nil)
	// Reuse a running torrent like the Kodi /play route; this is also how a re-POST from choosing_file keeps it.
	var t *bittorrent.Torrent
	if hash != "" {
		t = sessions.bts.GetTorrentByHash(hash)
	} else {
		t = sessions.bts.GetTorrentByURI(uri)
	}
	if t != nil {
		player.Params().ResumeHash = t.InfoHash()
		player.SetTorrent(t) // before the old session stops, so it doesn't remove the shared torrent
	}

	sess := &session{id: rand.Text(), player: player, hash: hash, state: PlaybackStateResolving, seen: time.Now()}
	sessions.Lock()
	sess.activate()
	view := sess.view(c.Request.Host)
	sessions.Unlock()

	reaper.Do(func() { go reapLoop() })
	go run(sess, t)
	c.JSON(http.StatusCreated, view)
}

// run buffers the session's file and records the outcome.
func run(sess *session, reused *bittorrent.Torrent) {
	p := sess.player
	if reused != nil && !reused.HasMetadata() {
		reused.WaitForMetadata(nil, reused.InfoHash())
	}
	err := p.Buffer()
	var files []PlaybackFile
	if errors.Is(err, bittorrent.ErrChooseFile) {
		files = candidates(p)
	}

	sessions.Lock()
	defer sessions.Unlock()
	if sess.state == PlaybackStateStopped || sess.state == PlaybackStateError {
		return // stop closed the player, or the stall watchdog failed the session
	}
	sess.settle(err, p.ChosenFile(), files, resumePoint(p.Params()))
	if sess.state == PlaybackStateError {
		go p.Close() // like the Kodi /play route after a failed Buffer
	}
}

// settle moves a resolving session to its state after Buffer returned err.
func (sess *session) settle(err error, file *bittorrent.File, files []PlaybackFile, resume *float64) {
	switch {
	case err == nil && file != nil:
		sess.state, sess.file, sess.resume = PlaybackStateReady, file, resume
	case errors.Is(err, bittorrent.ErrChooseFile) && len(files) > 0:
		sess.state, sess.files = PlaybackStateChoosingFile, files
	default:
		log.Warningf("Playback session %s failed: %v", sess.id, err)
		sess.state = PlaybackStateError
	}
}

// candidates lists the files the Kodi picker would offer; Index is the torrent file index (OriginalIndex).
func candidates(p *bittorrent.Player) []PlaybackFile {
	t := p.GetTorrent()
	choices, _, err := t.GetCandidateFiles(p)
	if err != nil {
		return nil
	}
	all := t.GetFiles()
	files := make([]PlaybackFile, 0, len(choices))
	for _, c := range choices {
		f := all[c.Index]
		files = append(files, PlaybackFile{Index: f.Index, Name: c.DisplayName, SizeBytes: f.Size})
	}
	return files
}

// resumePoint is the position the Kodi player would seek to: the stored resume, else the Kodi library or
// tracker progress that GetIdent found. None when the user turned Elementum's resume off ("by Kodi"): with no
// dialog to ask, External mode resumes like "Force".
func resumePoint(p *bittorrent.PlayerParams) *float64 {
	if config.Get().PlayResumeAction == 0 || p.ResumePlayback == bittorrent.ResumeNo {
		return nil
	}
	v := p.ResumePosition()
	if v < 0 {
		return nil
	}
	return &v
}

// activate makes sess the one active session, stopping the previous one; sessions must be locked.
func (sess *session) activate() {
	if old := sessions.active; old != nil {
		old.stop()
	}
	sessions.active = sess
	sessions.byID[sess.id] = sess
}

// stop ends the session; sessions must be locked. Close removes the torrent the way Kodi's stop does,
// following the keep/delete settings (with no Kodi host to ask, "ask" means keep).
func (sess *session) stop() {
	if sess.state == PlaybackStateStopped {
		return
	}
	sess.state = PlaybackStateStopped
	if sessions.active == sess {
		sessions.active = nil
	}
	if sess.player != nil {
		go sess.player.Close()
	}
}

func reapLoop() {
	for now := range time.Tick(5 * time.Second) {
		reap(now)
	}
}

// reap stops and forgets sessions without a status or progress call for idleTimeout.
func reap(now time.Time) {
	sessions.Lock()
	defer sessions.Unlock()
	for id, sess := range sessions.byID {
		if now.Sub(sess.seen) > idleTimeout {
			sess.stop()
			delete(sessions.byID, id)
		} else if sess.state == PlaybackStateResolving {
			sess.checkStall(now)
		}
	}
}

// stallLimit is how long a buffering torrent may make no progress; 0 means do not watch (the buffer timeout is
// off, or libtorrent is checking files, which shows no download).
func stallLimit(bufferTimeout, torrentState int) time.Duration {
	if bufferTimeout <= 0 || torrentState == bittorrent.StatusChecking {
		return 0
	}
	return max(time.Duration(bufferTimeout)*time.Second, 60*time.Second)
}

// checkStall fails a session whose torrent has its metadata but stopped making buffering progress; sessions
// must be locked. The closed player makes Buffer return, and the error stays. A session without a known info
// hash (an HTTP .torrent link) can't be found before Buffer returns, so it is not watched.
func (sess *session) checkStall(now time.Time) {
	t := sess.torrent()
	if t == nil || !t.HasMetadata() {
		return
	}
	limit := stallLimit(config.Get().BufferTimeout, t.GetState())
	if limit == 0 {
		sess.stall = stall{}
		return
	}
	if sess.stall.update(t.GetBufferProgress(), int(t.GetStatus().DownloadPayloadRate), now, limit) {
		log.Warningf("Playback session %s made no buffering progress for %s", sess.id, limit)
		sess.state = PlaybackStateError
		go sess.player.Close()
	}
}

func (sess *session) view(host string) PlaybackSession {
	v := PlaybackSession{Id: sess.id, State: sess.state, ResumeS: sess.resume}
	switch sess.state {
	case PlaybackStateChoosingFile:
		v.Files = &sess.files
	case PlaybackStateError:
		v.Error = &ErrorBody{Code: ErrorCodePlaybackFailed, Message: "the torrent could not be played"}
	case PlaybackStateReady:
		v.BufferProgress = 100
		u := streamURL(host, sess.id, sess.file.Index, time.Now().Add(linkTTL).Unix())
		v.StreamUrl = &u
	}
	if t := sess.torrent(); t != nil {
		st := t.GetStatus()
		v.DownloadRate, v.Seeds, v.Peers = int64(st.DownloadPayloadRate), int32(st.NumSeeds), int32(st.NumPeers)
		if v.State == PlaybackStateResolving && t.HasMetadata() {
			v.State = PlaybackStateBuffering
		}
		if v.State == PlaybackStateBuffering {
			v.BufferProgress = float32(min(max(t.GetBufferProgress(), 0), 100))
		}
	}
	return v
}

// torrent finds the session's running torrent, nil when there is none (yet).
func (sess *session) torrent() *bittorrent.Torrent {
	switch {
	case sess.player == nil || sess.state == PlaybackStateStopped || sess.state == PlaybackStateError:
		return nil
	case sess.hash != "":
		return sessions.bts.GetTorrentByHash(sess.hash)
	case sess.state != PlaybackStateResolving:
		return sess.player.GetTorrent() // Buffer has returned, so this no longer races
	}
	return nil
}

// GetPlayback reports the session state and keeps it alive.
func (Server) GetPlayback(c *gin.Context, id SessionIdParam) {
	sessions.Lock()
	sess := sessions.byID[id]
	var view PlaybackSession
	if sess != nil {
		if sess.state != PlaybackStateStopped {
			sess.seen = time.Now()
		}
		view = sess.view(c.Request.Host)
	}
	sessions.Unlock()
	if sess == nil {
		fail(c, sessionNotFound)
		return
	}
	c.JSON(http.StatusOK, view)
}

// DeletePlayback stops the session and removes its torrent.
func (Server) DeletePlayback(c *gin.Context, id SessionIdParam) {
	sessions.Lock()
	sess := sessions.byID[id]
	if sess != nil {
		sess.stop()
		delete(sessions.byID, id)
	}
	sessions.Unlock()
	if sess == nil {
		fail(c, sessionNotFound)
		return
	}
	c.Status(http.StatusNoContent)
}

// ReportPlaybackProgress keeps the session alive and, once playing, stores the resume point and scrobbles.
func (Server) ReportPlaybackProgress(c *gin.Context, id SessionIdParam) {
	b := body[PlaybackProgress](c)
	if b == nil {
		return
	}
	if b.PositionS < 0 || b.DurationS < 0 {
		fail(c, bad("position_s and duration_s must be 0 or more"))
		return
	}
	sessions.Lock()
	sess := sessions.byID[id]
	found := sess != nil && sess.state != PlaybackStateStopped
	if found {
		sess.seen = time.Now()
		if sess.state == PlaybackStateReady && sess.player != nil {
			sess.player.UpdateExternal(b.PositionS, b.DurationS, b.Paused)
		}
	}
	sessions.Unlock()
	if !found {
		fail(c, sessionNotFound)
		return
	}
	c.Status(http.StatusNoContent)
}

// sign is hex(HMAC-SHA256(key, id + "/" + file_index + "/" + exp)).
func sign(id string, fileIndex int, exp int64) string {
	m := hmac.New(sha256.New, linkKey)
	fmt.Fprintf(m, "%s/%d/%d", id, fileIndex, exp)
	return hex.EncodeToString(m.Sum(nil))
}

// streamURL is the signed getStream link on the host the client used to reach the API.
func streamURL(host, id string, fileIndex int, exp int64) string {
	return fmt.Sprintf("http://%s/api/stream/%s/%d?exp=%d&sig=%s", host, id, fileIndex, exp, sign(id, fileIndex, exp))
}

// streamFS is the file system GetStream serves from: the torrents, as for /files/.
var streamFS = func(method string) http.FileSystem { return bittorrent.NewTorrentFS(sessions.bts, method) }

// videoTypes are set explicitly: Go's MIME table lacks most video types and sniffing calls Matroska "video/webm".
var videoTypes = map[string]string{
	".mkv": "video/x-matroska", ".mp4": "video/mp4", ".m4v": "video/x-m4v", ".avi": "video/x-msvideo",
	".webm": "video/webm", ".ts": "video/mp2t", ".m2ts": "video/mp2t", ".mov": "video/quicktime",
	".wmv": "video/x-ms-wmv", ".mpg": "video/mpeg", ".mpeg": "video/mpeg", ".flv": "video/x-flv",
}

// GetStream serves the session's chosen file to a valid signed link, with the same Range-capable handler as
// /files/ (GET and HEAD). Every rejection is the same 403.
func (Server) GetStream(c *gin.Context, id SessionIdParam, fileIndex int, p GetStreamParams) {
	var path string
	if hmac.Equal([]byte(p.Sig), []byte(sign(id, fileIndex, p.Exp))) && time.Now().Unix() <= p.Exp {
		sessions.Lock()
		if sess := sessions.byID[id]; sess != nil && sess.state == PlaybackStateReady && sess.file.Index == fileIndex {
			path = sess.file.Path
		}
		sessions.Unlock()
	}
	if path == "" {
		fail(c, forbidden)
		return
	}
	if ct := videoTypes[strings.ToLower(filepath.Ext(path))]; ct != "" {
		c.Header("Content-Type", ct) // else FileServer picks one by extension or content
	}
	c.Header("Connection", "close") // as /files/
	r := c.Request.Clone(c.Request.Context())
	r.URL.Path, r.URL.RawPath = "/"+filepath.ToSlash(path), ""
	http.FileServer(streamFS(r.Method)).ServeHTTP(c.Writer, r)
}

var torrentFileName = regexp.MustCompile(`^[0-9a-f]{40}\.torrent$`)

// playableURI checks the client's uri, a trust boundary: adding a torrent reads any local path and fetches
// any http URL (including "|header=value" suffixes). A uri from the stream search cache is used as is; a
// magnet is rebuilt from its info hash, keeping only its name and udp/http(s) trackers; a local path must be a .torrent
// Elementum saved in its temp folder. Anything else is refused. It returns the uri to add and its info hash
// ("" when unknown).
func playableURI(uri string, cached []*bittorrent.TorrentFile, tempPath string) (string, string, bool) {
	hash := magnetHash(uri)
	for _, t := range cached {
		if t.URI == uri || (hash != "" && strings.EqualFold(t.InfoHash, hash)) {
			return t.URI, strings.ToLower(t.InfoHash), true
		}
	}
	if hash != "" {
		u, _ := url.Parse(uri) // magnetHash parsed it already
		q, keep := u.Query(), url.Values{}
		if dn := q.Get("dn"); dn != "" {
			keep.Set("dn", dn)
		}
		for _, tr := range q["tr"] {
			if scheme, _, _ := strings.Cut(tr, "://"); scheme == "udp" || scheme == "http" || scheme == "https" {
				keep.Add("tr", tr)
			}
		}
		magnet := "magnet:?xt=urn:btih:" + hash
		if len(keep) > 0 {
			magnet += "&" + keep.Encode()
		}
		return magnet, hash, true
	}
	if name := filepath.Base(uri); tempPath != "" && torrentFileName.MatchString(name) && uri == filepath.Join(tempPath, name) {
		return uri, strings.TrimSuffix(name, ".torrent"), true
	}
	return "", "", false
}

// magnetHash returns the lower-case hex info hash of a magnet with exactly one valid btih, else "".
func magnetHash(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "magnet" {
		return ""
	}
	xt := u.Query()["xt"]
	if len(xt) != 1 || !strings.HasPrefix(xt[0], "urn:btih:") {
		return ""
	}
	h := strings.TrimPrefix(xt[0], "urn:btih:")
	if b, err := hex.DecodeString(h); err == nil && len(b) == 20 {
		return hex.EncodeToString(b)
	}
	if b, err := base32.StdEncoding.DecodeString(strings.ToUpper(h)); err == nil && len(b) == 20 {
		return hex.EncodeToString(b)
	}
	return ""
}
