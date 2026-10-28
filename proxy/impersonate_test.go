package proxy

import (
	"net/http"
	"testing"
)

func TestDetectBrowser(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		key  string
	}{
		{
			"chrome windows",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
			"chrome|145.0.0.0|win",
		},
		{
			"chrome android",
			"Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Mobile Safari/537.36",
			"chrome|145.0.0.0|android",
		},
		{
			"chrome linux",
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
			"chrome|145.0.0.0|linux",
		},
		{
			"edge windows",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36 Edg/145.0.0.0",
			"edge|145.0.0.0|win",
		},
		{
			"firefox windows",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:157.0) Gecko/20100101 Firefox/157.0",
			"firefox|157.0|win",
		},
		{
			"safari mac",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.3 Safari/605.1.15",
			"safari|26.3|mac",
		},
		{"unknown", "SomeBot/1.0", ""},
		{"empty", "", ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key, build := detectBrowser(test.ua)
			if key != test.key {
				t.Fatalf("got key %q, want %q", key, test.key)
			}
			if build == nil {
				return
			}
			if _, err := build(); err != nil {
				t.Fatalf("could not build profile: %s", err)
			}
		})
	}
}

func TestImpersonateUserAgent(t *testing.T) {
	chromeUA := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"

	for _, test := range []struct {
		name string
		ua   string
		want string
	}{
		{"known browser", chromeUA, chromeUA},
		{"unknown browser", "SomeBot/1.0", defaultUserAgent},
		{"missing", "", defaultUserAgent},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://example.com", nil)
			if test.ua != "" {
				req.Header.Set("User-Agent", test.ua)
			}
			if impersonate(req) == nil {
				t.Fatal("expected an impersonating round tripper")
			}
			if got := req.UserAgent(); got != test.want {
				t.Fatalf("got User-Agent %q, want %q", got, test.want)
			}
		})
	}
}
