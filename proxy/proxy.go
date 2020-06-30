package proxy

import (
	"bytes"
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"

	"github.com/elgatito/elementum/config"

	"github.com/ElementumOrg/cfbypass"
	"github.com/elazarl/goproxy"
	logging "github.com/op/go-logging"
)

var (
	log = logging.MustGetLogger("proxy")

	// Proxy ...
	Proxy = goproxy.NewProxyHttpServer()

	// ProxyPort ...
	ProxyPort = 65222

	hostMatch = "^.*$"
)

// AlwaysHTTPMitm ...
var AlwaysHTTPMitm goproxy.FuncHttpsHandler = func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
	return &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: CustomTLS(&goproxy.GoproxyCa)}, host
}

var goproxySignerVersion = ":goroxy1"

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

	bodyBytes, _ := ioutil.ReadAll(req.Body)
	defer req.Body.Close()

	ctx.UserData = bodyBytes
	req.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))

	return req, nil
}

func handleResponse(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
	defer ctx.Req.Body.Close()

	if config.Get().InternalProxyLogging {
		dumpResponse(resp, ctx, true, config.Get().InternalProxyLoggingBody)
	} else {
		dumpResponse(resp, ctx, false, false)
	}

	if resp == nil {
		return resp
	}

	if cfResp, err := cfbypass.RunProxy(resp, ctx); err != nil {
		log.Warningf("Could not solve the CloudFlare challenge: %s", err)
	} else if cfResp != nil {
		return cfResp
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

	dump, _ := httputil.DumpResponse(resp, body)
	log.Debugf("RESPONSE:\n%s", dump)
}

// StartProxy starts HTTP/HTTPS proxy for debugging
func StartProxy() *http.Server {
	Proxy.OnRequest(goproxy.ReqHostMatches(regexp.MustCompile(hostMatch))).
		HandleConnect(AlwaysHTTPMitm)

	Proxy.OnRequest().DoFunc(handleRequest)
	Proxy.OnResponse().DoFunc(handleResponse)

	Proxy.Verbose = false
	Proxy.KeepDestinationHeaders = true

	if config.Get().ProxyURL != "" {
		proxyURL, _ := url.Parse(config.Get().ProxyURL)
		Proxy.Tr.Proxy = GetProxyURL(proxyURL)
		log.Debugf("Setting up proxy for internal proxy: %s", config.Get().ProxyURL)
	} else {
		Proxy.Tr.Proxy = GetProxyURL(nil)
	}

	if config.Get().InternalDNSEnabled {
		Proxy.Tr.Dial = CustomDial
	}

	cfbypass.LogEnabled = config.Get().InternalProxyLogging
	cfbypass.LogBodyEnabled = config.Get().InternalProxyLoggingBody

	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(ProxyPort),
		Handler: Proxy,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.Warningf("Could not start internal proxy: %s", err)
		}
	}()

	return srv
}
