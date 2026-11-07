package apiv1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
)

func ptr[T any](v T) *T { return &v }

func TestToStream(t *testing.T) {
	for name, tc := range map[string]struct {
		in   bittorrent.TorrentFile
		want Stream
	}{
		"all fields": {
			in: bittorrent.TorrentFile{
				URI: "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c", InfoHash: "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c",
				Name: "The.Matrix.1999.1080p.BluRay.x264", Provider: "[COLOR FFF14E13]YTS[/COLOR], [B]1337x[/B]",
				SizeParsed: 2147483648, Seeds: 120, Peers: 30, Multi: true, Language: "en",
				Resolution: bittorrent.Resolution1080p, RipType: bittorrent.RipBluRay,
				VideoCodec: bittorrent.CodecH264, AudioCodec: bittorrent.CodecDTSHDMA,
			},
			want: Stream{
				Uri: "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c", InfoHash: ptr("dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c"),
				Name: "The.Matrix.1999.1080p.BluRay.x264", Provider: "YTS, 1337x",
				SizeBytes: ptr(int64(2147483648)), Seeds: 120, Peers: 30, MultiProvider: true, Language: ptr("en"),
				Resolution: ptr(StreamResolutionN1080p), Rip: ptr(StreamRipBluRay),
				Codec: ptr(StreamCodecH264), Audio: ptr(StreamAudioDTSHDMA),
			},
		},
		"unknowns are null": {
			in:   bittorrent.TorrentFile{URI: "/tmp/x.torrent", Name: "x", Provider: "P"},
			want: Stream{Uri: "/tmp/x.torrent", Name: "x", Provider: "P"},
		},
		"out of range and wrong-kind codes are null": {
			in:   bittorrent.TorrentFile{Resolution: 99, RipType: -1, VideoCodec: bittorrent.CodecAAC, AudioCodec: bittorrent.CodecH265},
			want: Stream{},
		},
		"lowest table entries": {
			in:   bittorrent.TorrentFile{Resolution: bittorrent.Resolution240p, RipType: bittorrent.RipCam, VideoCodec: bittorrent.CodecXVid, AudioCodec: bittorrent.CodecMp3},
			want: Stream{Resolution: ptr(StreamResolutionN240p), Rip: ptr(StreamRipCamRip), Codec: ptr(StreamCodecXvid), Audio: ptr(StreamAudioMP3)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := toStream(&tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %s\nwant %s", jsonString(got), jsonString(tc.want))
			}
		})
	}
}

// Every table entry Elementum can produce must be a valid spec value.
func TestStreamTablesMatchSpec(t *testing.T) {
	for i := 1; i < len(bittorrent.Resolutions); i++ {
		if label[StreamResolution](bittorrent.Resolutions, i) == nil {
			t.Errorf("resolution %q not in spec", bittorrent.Resolutions[i])
		}
	}
	for i := 1; i < len(bittorrent.Rips); i++ {
		if label[StreamRip](bittorrent.Rips, i) == nil {
			t.Errorf("rip %q not in spec", bittorrent.Rips[i])
		}
	}
	for i := bittorrent.CodecXVid; i <= bittorrent.CodecH265; i++ {
		if label[StreamCodec](bittorrent.Codecs, i) == nil {
			t.Errorf("codec %q not in spec", bittorrent.Codecs[i])
		}
	}
	for i := bittorrent.CodecMp3; i <= bittorrent.CodecDTSHDMA; i++ {
		if label[StreamAudio](bittorrent.Codecs, i) == nil {
			t.Errorf("audio %q not in spec", bittorrent.Codecs[i])
		}
	}
}

func TestToStreamsEmptyIsArray(t *testing.T) {
	if s := jsonString(toStreams(nil)); s != "[]" {
		t.Errorf("got %s", s)
	}
}

// Unit tests have no Kodi host and the search cache is off, so these stop before any TMDB or provider call.
func TestStreamErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r)

	for path, want := range map[string]int{
		"/api/v1/movies/0/streams":                        http.StatusBadRequest,
		"/api/v1/shows/1/seasons/-1/episodes/1/streams":   http.StatusBadRequest,
		"/api/v1/shows/1/seasons/1/episodes/0/streams":    http.StatusBadRequest,
		"/api/v1/movies/603/streams":                      http.StatusBadGateway,
		"/api/v1/shows/1399/seasons/0/episodes/1/streams": http.StatusBadGateway,
		"/api/v1/shows/1399/seasons/1/episodes/1/streams": http.StatusBadGateway,
		"/api/v1/shows/1399/seasons/x/episodes/1/streams": http.StatusBadRequest,
	} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			var e Error
			if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || w.Code != want {
				t.Fatalf("status %d, want %d: %s", w.Code, want, w.Body)
			}
			code := ErrorCodeBadRequest
			if want == http.StatusBadGateway {
				code = ErrorCodeProvidersUnavailable
			}
			if e.Error.Code != code || e.Error.Message == "" {
				t.Errorf("error %+v, want code %s", e.Error, code)
			}
		})
	}
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
