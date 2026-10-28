package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/config"

	"github.com/elazarl/goproxy"
	logging "github.com/op/go-logging"
)

var (
	log = logging.MustGetLogger("proxy")

	// Proxy ...
	Proxy = goproxy.NewProxyHttpServer()

	// ProxyPort ...
	ProxyPort = 65222

	registerHandlers sync.Once
	proxyLock        sync.Mutex
	activeProxy      *CustomProxy
)

// CustomProxy stores http.Server with field showing there was an error while listening.
type CustomProxy struct {
	Server    *http.Server
	IsErrored bool
}

// AlwaysHTTPMitm ...
var AlwaysHTTPMitm goproxy.FuncHttpsHandler = func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
	return &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: CustomTLS(&goproxy.GoproxyCa)}, host
}

// CustomTLS ...
func CustomTLS(ca *tls.Certificate) func(host string, ctx *goproxy.ProxyCtx) (*tls.Config, error) {
	return func(host string, ctx *goproxy.ProxyCtx) (*tls.Config, error) {
		config := &tls.Config{
			Certificates:       []tls.Certificate{*ca},
			InsecureSkipVerify: true,
		}

		return config, nil
	}
}

func handleRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	// Removing these headers to ensure cloudflare is not taking these headers into account.
	req.Header.Del("Connection")
	req.Header.Del("Accept-Encoding")

	ctx.RoundTripper = impersonate(req)

	// req.Header.Del("Cookie")
	// req.Header.Del("Origin")

	if config.Get().InternalProxyLogging {
		dumpRequest(req, ctx, true, true)
	} else {
		dumpRequest(req, ctx, false, true)
	}

	bodyBytes, _ := io.ReadAll(req.Body)
	defer req.Body.Close()

	ctx.UserData = bodyBytes
	if len(bodyBytes) == 0 {
		req.Body = http.NoBody
	} else {
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	return req, nil
}

func handleResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	defer ctx.Req.Body.Close()

	if config.Get().InternalProxyLogging {
		dumpResponse(resp, ctx, true, config.Get().InternalProxyLoggingBody)
	} else {
		dumpResponse(resp, ctx, false, false)
	}

	return resp
}

func dumpRequest(req *http.Request, ctx *goproxy.ProxyCtx, details bool, body bool) {
	log.Debugf("[%d] --> %s %s", ctx.Session, req.Method, req.URL)

	if !details {
		return
	}

	if req == nil {
		log.Debugf("REQUEST: nil")
		return
	}

	dump, _ := httputil.DumpRequest(req, body)
	log.Debugf("REQUEST:\n%s", dump)
}

func dumpResponse(resp *http.Response, ctx *goproxy.ProxyCtx, details bool, body bool) {
	if resp != nil {
		log.Debugf("[%d] <-- %d %s", ctx.Session, resp.StatusCode, ctx.Req.URL.String())
	} else {
		log.Debugf("[%d] <-- ERR %s", ctx.Session, ctx.Req.URL.String())
		return
	}

	if !details {
		return
	}

	if resp == nil {
		log.Debugf("RESPONSE: nil")
		return
	}

	// Skip response dump for binary data
	if body && strings.Contains(resp.Header.Get("Content-Type"), "application/x-bittorrent") {
		body = false
	}

	dump, _ := httputil.DumpResponse(resp, body)
	log.Debugf("RESPONSE:\n%s", dump)
}

// StartProxy starts HTTP/HTTPS proxy for debugging.
// Repeated calls return the proxy that is already running.
func StartProxy() *CustomProxy {
	registerHandlers.Do(func() {
		Proxy.OnRequest().HandleConnect(AlwaysHTTPMitm)
		Proxy.OnRequest().DoFunc(handleRequest)
		Proxy.OnResponse().DoFunc(handleResponse)

		Proxy.Verbose = false
		Proxy.KeepDestinationHeaders = true
		Proxy.AllowHTTP2 = true
		// Tr has a custom TLSClientConfig/DialContext, so net/http won't try h2 upstream unless forced.
		Proxy.Tr.ForceAttemptHTTP2 = true
	})

	proxyLock.Lock()
	defer proxyLock.Unlock()

	if activeProxy != nil {
		return activeProxy
	}

	Proxy.Tr.Proxy = nil
	if config.Get().ProxyUseHTTP {
		if config.Get().ProxyURL != "" {
			proxyURL, _ := url.Parse(config.Get().ProxyURL)
			Proxy.Tr.Proxy = http.ProxyURL(proxyURL)

			log.Debugf("Setting up proxy for internal proxy: %s", config.Get().ProxyURL)
		} else if config.Get().AntizapretEnabled {
			Proxy.Tr.Proxy = antizapretProxy.ProxyURL

			log.Debugf("Setting up proxy for direct client to use Antizapret proxy dynamically.")
		}
	}

	if config.Get().InternalDNSEnabled {
		Proxy.Tr.DialContext = CustomDialContext
	}

	// Bind synchronously, so a port conflict cannot leave the app with an
	// undetectable, unreferenced proxy while a new proxy is reported as active.
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(ProxyPort))
	if err != nil {
		log.Warningf("Could not start internal proxy: %s", err)
		return &CustomProxy{IsErrored: true}
	}

	srv := &CustomProxy{
		Server: &http.Server{Handler: Proxy},
	}
	activeProxy = srv

	go func() {
		log.Infof("Starting internal proxy at :%d", ProxyPort)
		if err := srv.Server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Warningf("Could not start internal proxy: %s", err)
			srv.IsErrored = true
		}
	}()

	return srv
}

// StopProxy shuts down the running proxy, if any.
func StopProxy() {
	proxyLock.Lock()
	defer proxyLock.Unlock()

	p := activeProxy
	activeProxy = nil
	if p == nil || p.Server == nil {
		return
	}

	log.Infof("Stopping internal proxy")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Server.Shutdown(ctx); err != nil {
		p.Server.Close()
	}
}
