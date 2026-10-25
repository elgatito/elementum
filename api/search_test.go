package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/xbmc"
)

func TestWantsLinksList(t *testing.T) {
	for _, c := range []struct {
		ua, url string
		want    bool
	}{
		{"plugin.video.elementum", "/search?q=x", false},
		{"plugin.video.elementum", "/search?q=x&list=1", true},
		{"Mozilla/5.0", "/search?q=x", true},
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("GET", c.url, nil)
		ctx.Request.Header.Set("User-Agent", c.ua)
		if got := wantsLinksList(ctx); got != c.want {
			t.Errorf("%s %s: got %v, want %v", c.ua, c.url, got, c.want)
		}
	}
}

func TestRenderLinks(t *testing.T) {
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	renderLinks(ctx, []*bittorrent.TorrentFile{{URI: "magnet:?xt=1", Name: "Foo", Seeds: 5, Peers: 2, Resolution: 4, Size: "1 GB", Provider: "P"}},
		"plugin://plugin.video.elementum/play", "type", "search")

	var view xbmc.View
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Items) != 1 {
		t.Fatalf("got %d items", len(view.Items))
	}
	i := view.Items[0]
	if i.Path != "plugin://plugin.video.elementum/play?type=search&uri=magnet%3A%3Fxt%3D1" || !i.IsPlayable || i.Info.TagLine != "5 / 2 - 1080p - 1 GB" || i.Label2 != "P" {
		t.Errorf("unexpected item: %+v %+v", i, i.Info)
	}
}

func TestProviderIcon(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000")
	path := filepath.Join(t.TempDir(), "provider.png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.GET("/providericon/:id", ProviderIcon)
	get := func(url string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		return w
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/search", nil)
	ctx.Request.Header.Set("User-Agent", "plugin.video.elementum")
	// Kodi reads icon by path
	if got := providerIcon(ctx, path); got != path {
		t.Errorf("Kodi got %q, want %q", got, path)
	}

	ctx.Request.Header.Set("User-Agent", "Mozilla/5.0")
	url := providerIcon(ctx, path)
	if w := get(url); w.Code != 200 || w.Body.String() != string(png) {
		t.Errorf("%s: got %d %q", url, w.Code, w.Body.String())
	}
	// Only icons, given by providers, are served
	if w := get("/providericon/123"); w.Code != 404 {
		t.Errorf("unknown icon: got %d", w.Code)
	}
	// File is on another machine
	if w := get(providerIcon(ctx, "/no/such/icon.png")); w.Code != 404 {
		t.Errorf("missing file: got %d", w.Code)
	}
}
