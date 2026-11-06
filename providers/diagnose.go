package providers

import (
	"errors"
	"net/url"
	"time"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

var errResolveTimeout = errors.New("torrent file was not downloaded in time")

// Problem is an issue found in provider results
type Problem struct {
	Text    string
	Count   int
	Example string
}

// Result is a copy of what provider returned, taken before resolving, with a problem found in it
type Result struct {
	Name     string
	URI      string
	Size     string
	Provider string
	Seeds    int64
	Peers    int64
	// Problem is empty when Elementum can use the result
	Problem string
}

// Diagnosis is a result of a test search, made by a single provider
type Diagnosis struct {
	AddonID    string
	Payload    SearchObject
	Responded  bool
	Elapsed    time.Duration
	ParseError error
	Results    []*Result
	Usable     int
	Problems   []*Problem
	Requests   []proxy.RecordedRequest
}

// Diagnose runs a test search with a provider and checks what it returns.
// Requests, made by provider through internal proxy, are recorded by a tag in proxy URL.
func Diagnose(xbmcHost *xbmc.XBMCHost, callbackHost, addonID, method string, build func(*AddonSearcher) SearchObject) *Diagnosis {
	as := NewAddonSearcher(xbmcHost, callbackHost, addonID)
	d := &Diagnosis{AddonID: addonID, Payload: build(as)}

	tag := "diag" + util.RandomString(12)
	if u, err := url.Parse(d.Payload.General().InternalProxyURL); err == nil {
		// Python requests ignores user name without a password, so it is "tag:@host"
		u.User = url.UserPassword(tag, "")
		d.Payload.General().InternalProxyURL = u.String()
	}

	proxy.StartRecording(tag)
	start := time.Now()
	body, ok := as.callRaw(method, d.Payload)
	d.Elapsed = time.Since(start)
	d.Requests = proxy.StopRecording(tag)
	d.Responded = ok
	if !ok {
		return d
	}

	torrents := []*bittorrent.TorrentFile{}
	if d.ParseError = json.Unmarshal(body, &torrents); d.ParseError == nil {
		d.check(torrents)
	}
	return d
}

func (d *Diagnosis) check(torrents []*bittorrent.TorrentFile) {
	errs := make([]chan error, len(torrents))
	for i, t := range torrents {
		// Copy before Resolve starts writing into t
		d.Results = append(d.Results, &Result{Name: t.Name, URI: t.URI, Size: t.Size, Provider: t.Provider, Seeds: t.Seeds, Peers: t.Peers})
		errs[i] = make(chan error, 1)
		go func() { errs[i] <- t.Resolve() }()
	}
	expired := make(chan struct{})
	time.AfterFunc(trackerTimeout*2, func() { close(expired) })

	for i, t := range torrents {
		var err error
		select {
		case err = <-errs[i]:
		case <-expired:
			select {
			case err = <-errs[i]:
			default:
				err = errResolveTimeout
			}
		}

		r := d.Results[i]
		switch {
		case err == errResolveTimeout:
			// Resolve is still running and writes into t, so only the copy is read
			d.problem(r, "Torrent file was not downloaded in time", r.URI)
		case r.URI == "":
			d.problem(r, "Result has no 'uri'", r.Name)
		case err != nil:
			d.problem(r, "Torrent file can't be downloaded or parsed", r.URI+": "+err.Error())
		case t.InfoHash == "":
			d.problem(r, "Magnet link has no valid info hash", r.URI)
		case r.Name == "":
			d.Usable++
			d.problem(r, "Result has no 'name'", r.URI)
		default:
			d.Usable++
		}
	}
}

func (d *Diagnosis) problem(r *Result, text, example string) {
	r.Problem = text
	for _, p := range d.Problems {
		if p.Text == text {
			p.Count++
			return
		}
	}
	d.Problems = append(d.Problems, &Problem{Text: text, Count: 1, Example: example})
}
