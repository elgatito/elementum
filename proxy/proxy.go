package proxy

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

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
			PreferServerCipherSuites: false,
			Certificates:             []tls.Certificate{*ca},
			InsecureSkipVerify:       true,
		}

		return config, nil
	}
}

func handleRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	// Removing these headers to ensure cloudflare is not taking these headers into account.
	req.Header.Del("Connection")
	req.Header.Del("Accept-Encoding")

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
	req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

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

// StartProxy starts HTTP/HTTPS proxy for debugging
func StartProxy() *CustomProxy {
	Proxy.OnRequest().HandleConnect(AlwaysHTTPMitm)

	Proxy.OnRequest().DoFunc(handleRequest)
	Proxy.OnResponse().DoFunc(handleResponse)

	Proxy.Verbose = false
	Proxy.KeepDestinationHeaders = true

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

	srv := &CustomProxy{
		&http.Server{
			Addr:    ":" + strconv.Itoa(ProxyPort),
			Handler: Proxy,
		},
		false,
	}

	go func() {
		log.Infof("Starting internal proxy at :%d", ProxyPort)
		if err := srv.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Warningf("Could not start internal proxy: %s", err)
			srv.IsErrored = true
		}
	}()

	return srv
}
