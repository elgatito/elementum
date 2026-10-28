package proxy

import (
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/elazarl/goproxy"
	"github.com/saucesteals/mimic"
	"github.com/saucesteals/mimic/chromium"
	"github.com/saucesteals/mimic/firefox"
	"github.com/saucesteals/mimic/safari"
	tlsx "github.com/saucesteals/tlsx"
)

// defaultBrowserVersion is used for requests without a recognizable browser User-Agent.
const defaultBrowserVersion = "145.0.0.0"

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + defaultBrowserVersion + " Safari/537.36"

// maxImpersonationTransports bounds cached transports, since the User-Agent is caller-controlled.
const maxImpersonationTransports = 64

var (
	chromeVersionRegex  = regexp.MustCompile(`(?:Chrome|Chromium)/([0-9][0-9.]*)`)
	edgeVersionRegex    = regexp.MustCompile(`Edg[AM]?/([0-9][0-9.]*)`)
	firefoxVersionRegex = regexp.MustCompile(`Firefox/([0-9][0-9.]*)`)
	safariVersionRegex  = regexp.MustCompile(`Version/([0-9][0-9.]*)`)

	impersonateLock       sync.Mutex
	impersonateTransports = map[string]mimic.Transport{}
)

// impersonate returns a round tripper adopting the browser fingerprint from the
// request's User-Agent, or nil to keep the regular Go fingerprint. Requests with
// an unknown User-Agent are sent as the default browser.
func impersonate(req *http.Request) goproxy.RoundTripper {
	transport, matched := impersonateTransport(req.UserAgent())
	if transport == nil {
		return nil
	}
	if !matched {
		req.Header.Set("User-Agent", defaultUserAgent)
	}

	return goproxy.RoundTripperFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Response, error) {
		return transport.RoundTrip(req)
	})
}

// impersonateTransport returns a cached transport and whether the User-Agent
// matched a known browser.
func impersonateTransport(userAgent string) (mimic.Transport, bool) {
	key, build := detectBrowser(userAgent)
	matched := build != nil
	if !matched {
		key, build = "default", func() (*mimic.Profile, error) {
			return chromium.New(chromium.Config{
				Brand:    chromium.Chrome,
				Version:  defaultBrowserVersion,
				Platform: chromium.Linux,
			})
		}
	}

	impersonateLock.Lock()
	defer impersonateLock.Unlock()

	if transport, ok := impersonateTransports[key]; ok {
		return transport, matched
	}
	if len(impersonateTransports) >= maxImpersonationTransports {
		return nil, false
	}

	profile, err := build()
	if err != nil {
		log.Debugf("Not impersonating %q: %s", userAgent, err)
		return nil, false
	}

	transport, err := mimic.NewTransport(mimic.Config{
		Profile: profile,
		HTTP:    Proxy.Tr.Clone(),
		// The proxy is also used for hosts with broken certificates.
		TLS: &tlsx.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		log.Debugf("Not impersonating %q: %s", userAgent, err)
		return nil, false
	}

	impersonateTransports[key] = transport
	return transport, matched
}

// detectBrowser maps a User-Agent to a profile builder and a cache key.
// It returns a nil builder when the browser is unknown or unsupported by mimic.
func detectBrowser(userAgent string) (string, func() (*mimic.Profile, error)) {
	if version := edgeVersionRegex.FindStringSubmatch(userAgent); version != nil {
		v, platform := version[1], chromiumPlatform(userAgent)
		return "edge|" + v + "|" + string(platform), func() (*mimic.Profile, error) {
			return chromium.New(chromium.Config{Brand: chromium.Edge, Version: v, Platform: platform})
		}
	}

	if version := chromeVersionRegex.FindStringSubmatch(userAgent); version != nil {
		v, platform := version[1], chromiumPlatform(userAgent)
		return "chrome|" + v + "|" + string(platform), func() (*mimic.Profile, error) {
			return chromium.New(chromium.Config{Brand: chromium.Chrome, Version: v, Platform: platform})
		}
	}

	if version := firefoxVersionRegex.FindStringSubmatch(userAgent); version != nil {
		v, platform := version[1], firefoxPlatform(userAgent)
		return "firefox|" + v + "|" + string(platform), func() (*mimic.Profile, error) {
			return firefox.New(firefox.Config{Version: v, Platform: platform})
		}
	}

	if version := safariVersionRegex.FindStringSubmatch(userAgent); version != nil && strings.Contains(userAgent, "Safari/") {
		v, platform := version[1], safariPlatform(userAgent)
		return "safari|" + v + "|" + string(platform), func() (*mimic.Profile, error) {
			return safari.New(safari.Config{Version: v, Platform: platform})
		}
	}

	return "", nil
}

func chromiumPlatform(userAgent string) chromium.Platform {
	switch {
	case strings.Contains(userAgent, "Android"):
		return chromium.Android
	case strings.Contains(userAgent, "Windows"):
		return chromium.Windows
	case strings.Contains(userAgent, "Macintosh"), strings.Contains(userAgent, "Mac OS X"):
		return chromium.Mac
	default:
		return chromium.Linux
	}
}

func firefoxPlatform(userAgent string) firefox.Platform {
	switch {
	case strings.Contains(userAgent, "Android"):
		return firefox.Android
	case strings.Contains(userAgent, "Windows"):
		return firefox.Windows
	case strings.Contains(userAgent, "Macintosh"), strings.Contains(userAgent, "Mac OS X"):
		return firefox.Mac
	default:
		return firefox.Linux
	}
}

func safariPlatform(userAgent string) safari.Platform {
	switch {
	case strings.Contains(userAgent, "iPhone"):
		return safari.IPhone
	case strings.Contains(userAgent, "iPad"):
		return safari.IPad
	default:
		return safari.Mac
	}
}
