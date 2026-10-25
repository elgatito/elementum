package xbmc

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetXBMCHostWithContextCookie(t *testing.T) {
	XBMCHosts = []*XBMCHost{}
	local, _ := AddLocalXBMCHost("127.0.0.1")
	remote, _ := AddXBMCHost("192.168.1.50")

	for _, c := range []struct {
		ua, url, cookie string
		want            *XBMCHost
	}{
		{"Mozilla/5.0", "/search", "", local},
		{"Mozilla/5.0", "/search", "192.168.1.50", remote},
		// Query parameter chooses Kodi for a single request
		{"Mozilla/5.0", "/playuri?kodi=127.0.0.1", "192.168.1.50", local},
		// Only already connected hosts can be chosen
		{"Mozilla/5.0", "/search", "10.0.0.1", local},
		{"Mozilla/5.0", "/playuri?kodi=10.0.0.1", "", local},
		// Kodi requests are bound to the Kodi that sent them
		{"plugin.video.elementum", "/playuri?kodi=127.0.0.1", "127.0.0.1", nil},
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("GET", c.url, nil)
		ctx.Request.RemoteAddr = "192.168.1.50:1234"
		ctx.Request.Header.Set("User-Agent", c.ua)
		if c.cookie != "" {
			ctx.Request.Header.Set("Cookie", KodiCookie+"="+c.cookie)
		}
		if c.want == nil {
			c.want = remote
		}
		if got, _ := GetXBMCHostWithContext(ctx); got != c.want {
			t.Errorf("%s %s with cookie %q: got %v, want %v", c.ua, c.url, c.cookie, got, c.want)
		}
	}
}

func TestXBMCHostIgnoresIPFamily(t *testing.T) {
	XBMCHosts = []*XBMCHost{}
	local, _ := AddLocalXBMCHost("127.0.0.1")
	remote, _ := AddXBMCHost("192.168.1.50")

	for host, want := range map[string]*XBMCHost{
		"::1":                 local,
		"127.0.0.1":           local,
		"::ffff:127.0.0.1":    local,
		"::ffff:192.168.1.50": remote,
	} {
		if got, _ := GetXBMCHost(host); got != want {
			t.Errorf("%s: got %v, want %v", host, got, want)
		}
	}
	if len(XBMCHosts) != 2 {
		t.Errorf("got %d hosts, want 2", len(XBMCHosts))
	}
}
