package apiv1

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
)

const bbbHash = "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c"

func TestPlayableURI(t *testing.T) {
	temp := filepath.Join(os.TempDir(), "elementum")
	saved := filepath.Join(temp, bbbHash+".torrent")
	cached := []*bittorrent.TorrentFile{
		{URI: "https://provider.example/dl/42.torrent", InfoHash: "08ADA5A7A6183AAE1E09D831DF6748D566095A10"},
		{URI: "http://provider.example/x|Cookie=passkey"}, // providers may send headers; trusted only verbatim
	}

	for name, tc := range map[string]struct {
		uri, want, hash string
	}{
		"magnet keeps name and udp/http trackers only": {
			uri:  "magnet:?xt=urn:btih:" + strings.ToUpper(bbbHash) + "&dn=Big+Buck+Bunny&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337&tr=wss%3A%2F%2Ftracker.webtorrent.dev&tr=file%3A%2F%2F%2Fetc%2Fpasswd&ws=http%3A%2F%2F127.0.0.1%2Fadmin&x.pe=10.0.0.1%3A1",
			want: "magnet:?xt=urn:btih:" + bbbHash + "&dn=Big+Buck+Bunny&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337", hash: bbbHash,
		},
		"bare magnet":            {uri: "magnet:?xt=urn:btih:" + bbbHash, want: "magnet:?xt=urn:btih:" + bbbHash, hash: bbbHash},
		"base32 magnet":          {uri: "magnet:?xt=urn:btih:3WBFL3G4PSSV7MF37AJSHWDQMLNR63I4", want: "magnet:?xt=urn:btih:" + bbbHash, hash: bbbHash},
		"magnet found in cache":  {uri: "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10", want: cached[0].URI, hash: "08ada5a7a6183aae1e09d831df6748d566095a10"},
		"cached http":            {uri: cached[0].URI, want: cached[0].URI, hash: "08ada5a7a6183aae1e09d831df6748d566095a10"},
		"cached http w/ headers": {uri: cached[1].URI, want: cached[1].URI},
		"saved torrent file":     {uri: saved, want: saved, hash: bbbHash},
	} {
		t.Run(name, func(t *testing.T) {
			got, hash, ok := playableURI(tc.uri, cached, temp)
			if !ok || got != tc.want || hash != tc.hash {
				t.Errorf("got %q, %q, %v; want %q, %q", got, hash, ok, tc.want, tc.hash)
			}
		})
	}

	for name, uri := range map[string]string{
		"empty":                   "",
		"file scheme":             "file:///etc/passwd",
		"plugin scheme":           "plugin://plugin.video.elementum/play?uri=x",
		"relative path":           bbbHash + ".torrent",
		"relative dot path":       "./" + bbbHash + ".torrent",
		"dot-dot out of temp":     filepath.Join(temp, "sub") + "/../../" + bbbHash + ".torrent",
		"dot-dot inside temp":     temp + "/sub/../" + bbbHash + ".torrent",
		"absolute outside temp":   "/etc/passwd",
		"torrent outside temp":    "/tmp/" + bbbHash + ".torrent",
		"temp subfolder":          filepath.Join(temp, "sub", bbbHash+".torrent"),
		"temp non-hash name":      filepath.Join(temp, "movie.torrent"),
		"temp upper-case hash":    filepath.Join(temp, strings.ToUpper(bbbHash)+".torrent"),
		"uncached http":           "http://127.0.0.1:65220/shutdown",
		"uncached https":          "https://provider.example/dl/43.torrent",
		"cached http plus header": cached[0].URI + "|Cookie=x",
		"cached http case":        strings.ToUpper(cached[0].URI),
		"header injection":        "http://provider.example/x|User-Agent=evil",
		"short btih":              "magnet:?xt=urn:btih:dd8255ec",
		"non-hex btih":            "magnet:?xt=urn:btih:zz8255ecdc7ca55fb0bbf81323d87062db1f6d1c",
		"btih with header":        "magnet:?xt=urn:btih:" + bbbHash + "|Cookie=x",
		"two btih":                "magnet:?xt=urn:btih:" + bbbHash + "&xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10",
		"no xt":                   "magnet:?dn=x",
		"other urn":               "magnet:?xt=urn:sha1:" + bbbHash,
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			if got, hash, ok := playableURI(uri, cached, temp); ok || got != "" || hash != "" {
				t.Errorf("accepted as %q, %q", got, hash)
			}
		})
	}
	if _, _, ok := playableURI(saved, nil, ""); ok {
		t.Error("local path accepted without a temp path")
	}
}

func resetSessions(t *testing.T) {
	t.Helper()
	sessions.Lock()
	sessions.byID, sessions.active = map[string]*session{}, nil
	sessions.Unlock()
}

func addSession(id string, state PlaybackState) *session {
	sess := &session{id: id, state: state, seen: time.Now()}
	sessions.Lock()
	sess.activate()
	sessions.Unlock()
	return sess
}

func serve(r *gin.Engine, method, target string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "tv.example:65220"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func playbackEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, nil)
	return r
}

// Rejected before any torrent work, so the nil service is never reached.
func TestCreatePlaybackBadRequest(t *testing.T) {
	r := playbackEngine()
	magnet := "magnet:?xt=urn:btih:" + bbbHash
	for name, body := range map[string]string{
		"bad type":           `{"uri":"` + magnet + `","type":"show","tmdb_id":1}`,
		"bad tmdb_id":        `{"uri":"` + magnet + `","type":"movie","tmdb_id":0}`,
		"negative file":      `{"uri":"` + magnet + `","type":"movie","tmdb_id":1,"file_index":-1}`,
		"episode no season":  `{"uri":"` + magnet + `","type":"episode","tmdb_id":1,"episode":2}`,
		"episode no episode": `{"uri":"` + magnet + `","type":"episode","tmdb_id":1,"season":1}`,
		"episode zero":       `{"uri":"` + magnet + `","type":"episode","tmdb_id":1,"season":1,"episode":0}`,
		"local path":         `{"uri":"/etc/passwd","type":"movie","tmdb_id":1}`,
		"file uri":           `{"uri":"file:///etc/passwd","type":"movie","tmdb_id":1}`,
		"internal url":       `{"uri":"http://127.0.0.1:65220/shutdown|X=1","type":"movie","tmdb_id":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := serve(r, http.MethodPost, "/api/v1/playback", body)
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"bad_request"`) {
				t.Fatalf("got %d %s", w.Code, w.Body)
			}
			for _, leak := range []string{"/etc", "passwd", "127.0.0.1", "shutdown"} {
				if strings.Contains(w.Body.String(), leak) {
					t.Errorf("response echoes %q: %s", leak, w.Body)
				}
			}
		})
	}
}

func TestSettle(t *testing.T) {
	file := &bittorrent.File{Index: 3, Path: "a.mkv"}
	files := []PlaybackFile{{Index: 1, Name: "a.mkv", SizeBytes: 1}, {Index: 3, Name: "b.mkv", SizeBytes: 2}}
	resume := 42.0
	for name, tc := range map[string]struct {
		err   error
		file  *bittorrent.File
		files []PlaybackFile
		want  PlaybackState
	}{
		"buffered":                 {file: file, want: PlaybackStateReady},
		"ambiguous file":           {err: bittorrent.ErrChooseFile, files: files, want: PlaybackStateChoosingFile},
		"ambiguous, no candidates": {err: bittorrent.ErrChooseFile, want: PlaybackStateError},
		"failed":                   {err: errors.New("magnet timeout"), want: PlaybackStateError},
		"no file":                  {want: PlaybackStateError},
	} {
		t.Run(name, func(t *testing.T) {
			sess := &session{id: "S", state: PlaybackStateResolving}
			sess.settle(tc.err, tc.file, tc.files, &resume)
			if sess.state != tc.want {
				t.Fatalf("state %s, want %s", sess.state, tc.want)
			}
			v := sess.view("tv.example:65220")
			switch tc.want {
			case PlaybackStateReady:
				if v.StreamUrl == nil || !strings.HasPrefix(*v.StreamUrl, "http://tv.example:65220/api/v1/stream/S/3?exp=") || v.BufferProgress != 100 || *v.ResumeS != 42 {
					t.Errorf("ready view %+v", v)
				}
			case PlaybackStateChoosingFile:
				if v.Files == nil || len(*v.Files) != 2 || v.StreamUrl != nil {
					t.Errorf("choosing view %+v", v)
				}
			case PlaybackStateError:
				if v.Error == nil || v.Error.Code != ErrorCodePlaybackFailed || strings.Contains(v.Error.Message, "magnet") {
					t.Errorf("error view %+v", v.Error)
				}
			}
		})
	}
}

func TestSessionLifecycle(t *testing.T) {
	resetSessions(t)
	r := playbackEngine()

	first := addSession("FIRST", PlaybackStateChoosingFile)
	second := addSession("SECOND", PlaybackStateResolving)
	if first.state != PlaybackStateStopped || sessions.active != second {
		t.Fatal("a new session must stop the active one")
	}

	var got PlaybackSession
	w := serve(r, http.MethodGet, "/api/v1/playback/FIRST", "")
	if json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || got.State != PlaybackStateStopped {
		t.Errorf("replaced session: %d %s", w.Code, w.Body)
	}
	if w := serve(r, http.MethodGet, "/api/v1/playback/NOPE", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown session: %d", w.Code)
	}

	// Status and progress calls keep the session alive.
	second.seen = time.Now().Add(-time.Hour)
	serve(r, http.MethodGet, "/api/v1/playback/SECOND", "")
	if time.Since(second.seen) > time.Second {
		t.Error("GET did not refresh the session")
	}
	second.seen = time.Now().Add(-time.Hour)
	if w := serve(r, http.MethodPost, "/api/v1/playback/SECOND/progress", `{"position_s":10,"duration_s":100,"paused":false}`); w.Code != http.StatusNoContent || time.Since(second.seen) > time.Second {
		t.Errorf("progress: %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{"position_s":-1,"duration_s":100,"paused":false}`, `{"position_s":1,"duration_s":-1,"paused":false}`, `{`} {
		if w := serve(r, http.MethodPost, "/api/v1/playback/SECOND/progress", body); w.Code != http.StatusBadRequest {
			t.Errorf("progress %s: %d", body, w.Code)
		}
	}
	if w := serve(r, http.MethodPost, "/api/v1/playback/FIRST/progress", `{"position_s":1,"duration_s":2,"paused":true}`); w.Code != http.StatusNotFound {
		t.Errorf("progress on stopped session: %d", w.Code)
	}

	// The reaper stops and forgets sessions idle for more than 60 s, and only those.
	reap(time.Now().Add(idleTimeout - time.Second))
	if second.state == PlaybackStateStopped {
		t.Fatal("reaped too early")
	}
	reap(time.Now().Add(idleTimeout + time.Second))
	if second.state != PlaybackStateStopped || len(sessions.byID) != 0 || sessions.active != nil {
		t.Fatalf("reaper left %v", sessions.byID)
	}

	third := addSession("THIRD", PlaybackStateReady)
	if w := serve(r, http.MethodDelete, "/api/v1/playback/THIRD", ""); w.Code != http.StatusNoContent || third.state != PlaybackStateStopped {
		t.Errorf("delete: %d", w.Code)
	}
	if w := serve(r, http.MethodDelete, "/api/v1/playback/THIRD", ""); w.Code != http.StatusNotFound {
		t.Errorf("second delete: %d", w.Code)
	}
}

func TestStreamLink(t *testing.T) {
	resetSessions(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "Big Buck Bunny"), 0o755)
	os.WriteFile(filepath.Join(dir, "Big Buck Bunny", "bbb.mkv"), []byte("0123456789"), 0o644)
	oldFS := streamFS
	streamFS = func(string) http.FileSystem { return http.Dir(dir) }
	t.Cleanup(func() { streamFS = oldFS })

	// Basic auth is on, the stream must not need it.
	oldArgs := config.Args
	config.Args.LocalLogin, config.Args.LocalPassword = "user", "pass"
	t.Cleanup(func() { config.Args = oldArgs })

	r := playbackEngine()
	sess := addSession("SID", PlaybackStateReady)
	sess.file = &bittorrent.File{Index: 2, Path: filepath.Join("Big Buck Bunny", "bbb.mkv")}

	exp := time.Now().Add(time.Hour).Unix()
	link := func(id string, index int, exp int64, sig string) string {
		return fmt.Sprintf("/api/v1/stream/%s/%d?exp=%d&sig=%s", id, index, exp, sig)
	}
	valid := link("SID", 2, exp, sign("SID", 2, exp))

	u, _ := url.Parse(streamURL("tv.example:65220", "SID", 2, exp))
	if u.Host != "tv.example:65220" || u.RequestURI() != valid {
		t.Fatalf("streamURL %s, want path %s", u, valid)
	}

	req := httptest.NewRequest(http.MethodGet, valid, nil)
	req.Header.Set("Range", "bytes=2-5")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "2345" || w.Header().Get("Content-Type") != "video/x-matroska" {
		t.Fatalf("ranged GET: %d %q %v", w.Code, w.Body, w.Header())
	}
	if w := serve(r, http.MethodHead, valid, ""); w.Code != http.StatusOK || w.Header().Get("Content-Length") != "10" || w.Body.Len() != 0 || w.Header().Get("Content-Type") != "video/x-matroska" {
		t.Fatalf("HEAD: %d %v %q", w.Code, w.Header(), w.Body)
	}

	other := addSession("OTHER", PlaybackStateReady) // replaces SID, so this also checks a stopped session
	other.file = sess.file
	sessions.Lock()
	sessions.byID["SID"] = sess // still known, but stopped
	sessions.Unlock()
	past := time.Now().Add(-time.Second).Unix()
	sig := sign("OTHER", 2, exp)
	flipped := []byte(sig)
	flipped[0] ^= 1
	for name, target := range map[string]string{
		"stopped session":     valid,
		"tampered sig":        link("OTHER", 2, exp, string(flipped)),
		"upper-case sig":      link("OTHER", 2, exp, strings.ToUpper(sig)),
		"tampered id":         link("OTHEX", 2, exp, sig),
		"tampered exp":        link("OTHER", 2, exp+1, sig),
		"tampered file_index": link("OTHER", 3, exp, sig),
		"other file signed":   link("OTHER", 3, exp, sign("OTHER", 3, exp)),
		"expired":             link("OTHER", 2, past, sign("OTHER", 2, past)),
		"unknown session":     link("GONE", 2, exp, sign("GONE", 2, exp)),
		"missing sig":         fmt.Sprintf("/api/v1/stream/OTHER/2?exp=%d", exp),
		"non-numeric exp":     "/api/v1/stream/OTHER/2?exp=soon&sig=" + sig,
		"non-integer index":   fmt.Sprintf("/api/v1/stream/OTHER/2.5?exp=%d&sig=%s", exp, sig),
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := serve(r, method, target, "")
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s: %d", method, name, w.Code)
			} else if method == http.MethodGet && !strings.Contains(w.Body.String(), `"code":"forbidden","message":"forbidden"`) {
				t.Errorf("%s body %s", name, w.Body)
			}
		}
	}
	if w := serve(r, http.MethodGet, link("OTHER", 2, exp, sig), ""); w.Code != http.StatusOK {
		t.Errorf("valid link of the new session: %d", w.Code)
	}
}

func TestResumePoint(t *testing.T) {
	old := config.Get().PlayResumeAction
	t.Cleanup(func() { config.Get().PlayResumeAction = old })
	stored, tracked := &uid.Resume{Position: 300}, &uid.Resume{Position: 120}
	for name, tc := range map[string]struct {
		action int
		p      bittorrent.PlayerParams
		want   float64 // 0 = nil
	}{
		"stored first":      {action: 1, p: bittorrent.PlayerParams{ResumePlayback: bittorrent.ResumeYes, StoredResume: stored, Resume: tracked}, want: 300},
		"tracker progress":  {action: 2, p: bittorrent.PlayerParams{Resume: tracked}, want: 120},
		"resume by Kodi":    {action: 0, p: bittorrent.PlayerParams{ResumePlayback: bittorrent.ResumeYes, StoredResume: stored}},
		"resume declined":   {action: 1, p: bittorrent.PlayerParams{ResumePlayback: bittorrent.ResumeNo, StoredResume: stored}},
		"nothing to resume": {action: 1, p: bittorrent.PlayerParams{StoredResume: &uid.Resume{}}},
	} {
		config.Get().PlayResumeAction = tc.action
		got := resumePoint(&tc.p)
		if (got == nil) != (tc.want == 0) || (got != nil && *got != tc.want) {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}
