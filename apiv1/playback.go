package apiv1

import (
	"context"
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

	forbidden       = newError(ErrorCodeForbidden, "forbidden")
	badURI          = newError(ErrorCodeBadRequest, "uri is not a playable stream")
	sessionNotFound = newError(ErrorCodeNotFound, "session not found")
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
func (Server) CreatePlayback(ctx context.Context, req CreatePlaybackRequestObject) (CreatePlaybackResponseObject, error) {
	b := req.Body
	bad := func(msg string) (CreatePlaybackResponseObject, error) {
		return CreatePlayback400JSONResponse{BadRequestJSONResponse(newError(ErrorCodeBadRequest, msg))}, nil
	}
	if e := checkParams(nil, nil, b.Type); e != nil {
		return CreatePlayback400JSONResponse{BadRequestJSONResponse(*e)}, nil
	}
	if b.TmdbId < 1 || (b.FileIndex != nil && *b.FileIndex < 0) {
		return bad("tmdb_id must be 1 or more, file_index 0 or more")
	}

	// Same params as the Kodi /play route; for an episode TMDBId is the episode's id and ShowID the show's.
	params := bittorrent.PlayerParams{
		External: true, ContentType: string(b.Type), TMDBId: b.TmdbId,
		OriginalIndex: -1, FileIndex: -1, NextOriginalIndex: -1, NextFileIndex: -1, MinSize: -1, KodiPosition: -1,
	}
	cacheKey := strconv.Itoa(b.TmdbId)
	if b.Type == PlaybackTypeEpisode {
		if b.Season == nil || b.Episode == nil || *b.Season < 0 || *b.Episode < 1 {
			return bad("season (0 or more) and episode (1 or more) are required for an episode")
		}
		cacheKey = fmt.Sprintf("%d_%d_%d", b.TmdbId, *b.Season, *b.Episode)
		params.ShowID, params.Season, params.Episode = b.TmdbId, *b.Season, *b.Episode
	}
	if b.FileIndex != nil {
		params.OriginalIndex = *b.FileIndex
	}

	cached, _ := providers.GetCachedTorrents(cacheKey)
	uri, hash, ok := playableURI(b.Uri, cached, config.Get().TemporaryPath)
	if !ok {
		return CreatePlayback400JSONResponse{BadRequestJSONResponse(badURI)}, nil
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
	view := sess.view(ctx.(*gin.Context).Request.Host)
	sessions.Unlock()

	reaper.Do(func() { go reapLoop() })
	go run(sess, t)
	return CreatePlayback201JSONResponse(view), nil
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
	if sess.state == PlaybackStateStopped {
		return // stop closed the player
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
	if r := p.StoredResume; r != nil && r.Position > 0 {
		v := r.Position
		return &v
	}
	if r := p.Resume; r != nil && r.Position > 0 {
		v := r.Position
		return &v
	}
	return nil
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
		}
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
func (Server) GetPlayback(ctx context.Context, req GetPlaybackRequestObject) (GetPlaybackResponseObject, error) {
	sessions.Lock()
	defer sessions.Unlock()
	sess := sessions.byID[req.Id]
	if sess == nil {
		return GetPlayback404JSONResponse{NotFoundJSONResponse(sessionNotFound)}, nil
	}
	if sess.state != PlaybackStateStopped {
		sess.seen = time.Now()
	}
	return GetPlayback200JSONResponse(sess.view(ctx.(*gin.Context).Request.Host)), nil
}

// DeletePlayback stops the session and removes its torrent.
func (Server) DeletePlayback(_ context.Context, req DeletePlaybackRequestObject) (DeletePlaybackResponseObject, error) {
	sessions.Lock()
	defer sessions.Unlock()
	sess := sessions.byID[req.Id]
	if sess == nil {
		return DeletePlayback404JSONResponse{NotFoundJSONResponse(sessionNotFound)}, nil
	}
	sess.stop()
	delete(sessions.byID, req.Id)
	return DeletePlayback204Response{}, nil
}

// ReportPlaybackProgress keeps the session alive and, once playing, stores the resume point and scrobbles.
func (Server) ReportPlaybackProgress(_ context.Context, req ReportPlaybackProgressRequestObject) (ReportPlaybackProgressResponseObject, error) {
	if req.Body.PositionS < 0 || req.Body.DurationS < 0 {
		return ReportPlaybackProgress400JSONResponse{BadRequestJSONResponse(newError(ErrorCodeBadRequest, "position_s and duration_s must be 0 or more"))}, nil
	}
	sessions.Lock()
	defer sessions.Unlock()
	sess := sessions.byID[req.Id]
	if sess == nil || sess.state == PlaybackStateStopped {
		return ReportPlaybackProgress404JSONResponse{NotFoundJSONResponse(sessionNotFound)}, nil
	}
	sess.seen = time.Now()
	if sess.state == PlaybackStateReady && sess.player != nil {
		sess.player.UpdateExternal(req.Body.PositionS, req.Body.DurationS, req.Body.Paused)
	}
	return ReportPlaybackProgress204Response{}, nil
}

// sign is hex(HMAC-SHA256(key, id + "/" + file_index + "/" + exp)).
func sign(id string, fileIndex int, exp int64) string {
	m := hmac.New(sha256.New, linkKey)
	fmt.Fprintf(m, "%s/%d/%d", id, fileIndex, exp)
	return hex.EncodeToString(m.Sum(nil))
}

// streamURL is the signed getStream link on the host the client used to reach the API.
func streamURL(host, id string, fileIndex int, exp int64) string {
	return fmt.Sprintf("http://%s/api/v1/stream/%s/%d?exp=%d&sig=%s", host, id, fileIndex, exp, sign(id, fileIndex, exp))
}

// GetStream serves the session's chosen file to a valid signed link. Every rejection is the same 403.
func (Server) GetStream(ctx context.Context, req GetStreamRequestObject) (GetStreamResponseObject, error) {
	deny := GetStream403JSONResponse{ForbiddenJSONResponse(forbidden)}
	if !hmac.Equal([]byte(req.Params.Sig), []byte(sign(req.Id, req.FileIndex, req.Params.Exp))) || time.Now().Unix() > req.Params.Exp {
		return deny, nil
	}
	sessions.Lock()
	sess := sessions.byID[req.Id]
	var path string
	if sess != nil && sess.state == PlaybackStateReady && sess.file.Index == req.FileIndex {
		path = sess.file.Path
	}
	sessions.Unlock()
	if path == "" {
		return deny, nil
	}
	return streamFile{req: ctx.(*gin.Context).Request, path: path}, nil
}

// streamFS is the file system getStream serves from: the torrents, as for /files/.
var streamFS = func(method string) http.FileSystem { return bittorrent.NewTorrentFS(sessions.bts, method) }

// videoTypes are set explicitly: Go's MIME table lacks most video types and sniffing calls Matroska "video/webm".
var videoTypes = map[string]string{
	".mkv": "video/x-matroska", ".mp4": "video/mp4", ".m4v": "video/x-m4v", ".avi": "video/x-msvideo",
	".webm": "video/webm", ".ts": "video/mp2t", ".m2ts": "video/mp2t", ".mov": "video/quicktime",
	".wmv": "video/x-ms-wmv", ".mpg": "video/mpeg", ".mpeg": "video/mpeg", ".flv": "video/x-flv",
}

// streamFile serves a torrent file with the same Range-capable handler as /files/ (GET and HEAD).
type streamFile struct {
	req  *http.Request
	path string
}

func (f streamFile) VisitGetStreamResponse(w http.ResponseWriter) error {
	if ct := videoTypes[strings.ToLower(filepath.Ext(f.path))]; ct != "" {
		w.Header().Set("Content-Type", ct) // else FileServer picks one by extension or content
	}
	w.Header().Set("Connection", "close") // as /files/
	r := f.req.Clone(f.req.Context())
	r.URL.Path, r.URL.RawPath = "/"+filepath.ToSlash(f.path), ""
	http.FileServer(streamFS(r.Method)).ServeHTTP(w, r)
	return nil
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
