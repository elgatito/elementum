package proxy

import (
	"errors"
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

	// impersonateLock also guards Proxy.Tr, transports are cloned from it
	impersonateLock       sync.Mutex
	impersonateTransports = map[string]mimic.Transport{}

	errNoImpersonation = errors.New("proxy: no browser profile to impersonate")
)

// impersonate returns a round tripper adopting the browser fingerprint from the
// request's User-Agent. Requests with an unknown or unsupported User-Agent are
// sent as the default browser, never with the Go TLS fingerprint.
func impersonate(req *http.Request) goproxy.RoundTripper {
	transport, matched := impersonateTransport(req.UserAgent())
	if !matched {
		req.Header.Set("User-Agent", defaultUserAgent)
	}

	return goproxy.RoundTripperFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Response, error) {
		if transport == nil {
			return nil, errNoImpersonation
		}
		return transport.RoundTrip(req)
	})
}

// impersonateTransport returns a cached transport and whether the User-Agent
// matched a supported browser. Otherwise it returns the default browser transport.
func impersonateTransport(userAgent string) (mimic.Transport, bool) {
	impersonateLock.Lock()
	defer impersonateLock.Unlock()

	if key, build := detectBrowser(userAgent); build != nil {
		if transport := cachedTransport(key, build); transport != nil {
			return transport, true
		}
	}

	return cachedTransport("default", func() (*mimic.Profile, error) {
		return chromium.New(chromium.Config{
			Brand:    chromium.Chrome,
			Version:  defaultBrowserVersion,
			Platform: chromium.Linux,
		})
	}), false
}

// cachedTransport returns the transport for a key, building it once. Failed
// builds are cached as nil, so an unsupported browser is reported only once.
func cachedTransport(key string, build func() (*mimic.Profile, error)) mimic.Transport {
	if transport, ok := impersonateTransports[key]; ok {
		return transport
	}
	// The default transport is the fallback for everything else, so it is never refused
	if key != "default" && len(impersonateTransports) >= maxImpersonationTransports {
		return nil
	}

	transport, err := newTransport(build)
	if err != nil {
		log.Warningf("Cannot impersonate %s, sending as Chrome %s instead, cookies bound to that browser will not work: %s", key, defaultBrowserVersion, err)
	}
	impersonateTransports[key] = transport
	return transport
}

func newTransport(build func() (*mimic.Profile, error)) (mimic.Transport, error) {
	profile, err := build()
	if err != nil {
		return nil, err
	}

	return mimic.NewTransport(mimic.Config{
		Profile: profile,
		HTTP:    Proxy.Tr.Clone(),
		// The proxy is also used for hosts with broken certificates.
		TLS: &tlsx.Config{InsecureSkipVerify: true},
	})
}

// resetTransports drops cached transports, so new ones are cloned from the updated proxy transport.
// Callers hold impersonateLock.
func resetTransports() {
	for _, transport := range impersonateTransports {
		if transport != nil {
			transport.CloseIdleConnections()
		}
	}
	impersonateTransports = map[string]mimic.Transport{}
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
