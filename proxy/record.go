package proxy

import (
	"net/http"
	"sync"

	"github.com/elazarl/goproxy"
)

// RecordedRequest is a request, that went through internal proxy with a recorded tag
type RecordedRequest struct {
	Method string
	URL    string
	Status int
	Error  string
}

type recording struct {
	mu       sync.Mutex
	requests []RecordedRequest
}

// recordings maps a tag (proxy user name, set by a client in proxy URL) to its recording
var recordings sync.Map

// StartRecording records requests, made with "tag" as proxy user name, like http://tag:@127.0.0.1:65222
func StartRecording(tag string) {
	recordings.Store(tag, &recording{})
}

// StopRecording returns requests recorded for the tag
func StopRecording(tag string) []RecordedRequest {
	v, ok := recordings.LoadAndDelete(tag)
	if !ok {
		return nil
	}
	r := v.(*recording)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests
}

// proxyUser returns user name from Proxy-Authorization header, sent for plain requests and for CONNECT
func proxyUser(req *http.Request) string {
	if req == nil {
		return ""
	}
	user, _, _ := (&http.Request{Header: http.Header{"Authorization": req.Header.Values("Proxy-Authorization")}}).BasicAuth()
	return user
}

func record(resp *http.Response, ctx *goproxy.ProxyCtx) {
	tag, _ := ctx.UserData.(string)
	if tag == "" || ctx.Req == nil {
		return
	}
	v, ok := recordings.Load(tag)
	if !ok {
		return
	}

	req := RecordedRequest{Method: ctx.Req.Method, URL: ctx.Req.URL.String()}
	if resp != nil {
		req.Status = resp.StatusCode
	}
	if ctx.Error != nil {
		req.Error = ctx.Error.Error()
	}

	r := v.(*recording)
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
}
