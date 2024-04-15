package reqapi

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/dustin/go-humanize"
	"github.com/goccy/go-json"
	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/trace"
)

type Request struct {
	trace.Tracer
	API *API

	Method      string
	URL         string
	Params      url.Values  `msg:"-"`
	Header      http.Header `msg:"-"`
	Payload     *bytes.Buffer
	Description string

	Retry        int
	RetryBackoff time.Duration

	ResponseError      error
	ResponseStatus     string
	ResponseStatusCode int
	ResponseHeader     http.Header
	ResponseBody       *bytes.Buffer
	ResponseSize       uint64

	Result any

	Cache            bool
	CacheExpire      time.Duration
	CacheForceExpire bool
	CacheKey         string
	cachePending     bool

	locker          util.Unlocker
	originalPayload []byte
}

type CacheEntry struct {
	Header     http.Header `json:"header"`
	Body       []byte      `json:"body"`
	Status     string      `json:"status"`
	StatusCode int         `json:"statuscode"`
}

func (r *Request) Prepare() (err error) {
	r.URL = r.API.GetURL(r.URL)

	if r.Method == "" {
		if r.Payload != nil {
			r.Method = "POST"
		} else {
			r.Method = "GET"
		}
	}

	if r.Header == nil {
		r.Header = http.Header{}
	}

	return nil
}

func (r *Request) requestKey() string {
	params, _ := url.QueryUnescape(r.Params.Encode())
	return fmt.Sprintf("%s%s.%s?%s", r.API.Ident, "reqapi", r.URL, params)
}

func (r *Request) Lock() error {
	r.locker = locker.Lock(r.requestKey())
	return nil
}

func (r *Request) Unlock() error {
	r.locker.Unlock()
	return nil
}

func (r *Request) CacheRead() error {
	if r.CacheKey == "" {
		r.CacheKey = r.requestKey()
	}

	if r.CacheForceExpire {
		return errors.New("cache read forced")
	}

	cacheStore := cache.NewDBStore()
	data, err := cacheStore.GetBytes(r.CacheKey)
	r.Stage("CacheReadBytes")
	if err != nil {
		return err
	}

	if c, err := r.UnmarshalCache(bytes.NewBuffer(data)); err == nil && c != nil {
		r.Stage("CacheReadUnmarshalCache")
		r.ApplyCache(c)

		if err = r.Unmarshal(bytes.NewBuffer(c.Body)); err != nil {
			r.Stage("CacheReadUnmarshal")
			return err
		}
	}
	r.Stage("CacheReadUnmarshal")

	return err
}

func (r *Request) CacheWrite() error {
	data, err := r.MarshalCache()
	r.Stage("CacheWriteMarshal")
	if err != nil {
		return err
	}
	cacheExpire := r.CacheExpire
	if cacheExpire == 0 {
		cacheExpire = cache.CacheExpireMedium
	}

	cacheStore := cache.NewDBStore()
	err = cacheStore.SetBytes(r.CacheKey, data, cacheExpire)
	r.Stage("CacheWriteBytes")
	return err
}

func (r *Request) Do() (err error) {
	defer perf.ScopeTimer()()

	r.Create()
	defer func() {
		// We should also cache 404 requests to avoid making them again, at least for cache period
		if r.Cache && r.cachePending && (err == nil || err == util.ErrNotFound) {
			go func() {
				err = r.CacheWrite()
				r.Error(err)
				r.Stage("CacheWrite")
				r.Unlock()
				r.LogStatus()
			}()
		} else {
			r.Error(err)
			r.Unlock()
			r.LogStatus()
		}
	}()

	if r.API == nil {
		err = errors.New("API not defined")
		return
	}

	// Do internal preparations
	if err = r.Prepare(); err != nil {
		return
	}

	// Lock execution for same requests.
	// If cache for this request is enabled -
	// 		it will be unlocked only after cache is written.
	r.Lock()

	if r.Cache {
		// Try to read result from cache
		if err = r.CacheRead(); err == nil {
			return err
		}
		r.Stage("CacheRead")
		r.cachePending = true
	}

	req := &napping.Request{
		Url:                 r.URL,
		Method:              r.Method,
		Params:              &r.Params,
		Header:              &r.Header,
		CaptureResponseBody: true,
	}

	// Apply body payload to request
	if r.Payload != nil {
		// Save payload for optional logging
		r.originalPayload = r.Payload.Bytes()
		r.Payload = bytes.NewBuffer(r.originalPayload)

		req.Payload = r.Payload
		req.RawPayload = true
	}

	var resp *napping.Response

	// Run request with rate limiter
	r.API.RateLimiter.Call(func() error {
		r.Stage("Request")

		for {
			resp, err = r.API.GetSession().Send(req)

			r.ApplyResponse(resp)

			if err != nil {
				log.Errorf("Failed to make request to %s for %s with %+v: %s", r.URL, r.Description, r.Params, err)
				return err
			} else if r.ResponseStatusCode == 429 {
				log.Warningf("Rate limit exceeded getting %s with %+v on %s, cooling down...", r.Description, r.Params, r.URL)
				r.API.RateLimiter.CoolDown(r.ResponseHeader)
				err = util.ErrExceeded
				r.Error(err)
				return err
			} else if r.ResponseStatusCode == 404 {
				log.Errorf("Bad status getting %s with %+v on %s: %d/%s", r.Description, r.Params, r.URL, r.ResponseStatusCode, r.ResponseStatus)
				err = util.ErrNotFound
				r.Error(err)
				return err
			} else if r.ResponseStatusCode == 403 && r.API.RetriesLeft > 0 {
				r.API.RetriesLeft--
				log.Warningf("Not authorized to get %s with %+v on %s, having %d retries left ...", r.Description, r.Params, r.URL, r.API.RetriesLeft)
				continue
			} else if r.ResponseStatusCode < 200 || r.ResponseStatusCode >= 300 {
				log.Errorf("Bad status getting %s with %+v on %s: %d/%s", r.Description, r.Params, r.URL, r.ResponseStatusCode, r.ResponseStatus)
				err = util.ErrHTTP
				r.Error(err)
				return err
			}

			break
		}

		err = nil
		return nil
	})

	r.Stage("Response")

	if resp != nil && resp.ResponseBody != nil && r.ResponseError == nil {
		r.Size(uint64(resp.ResponseBody.Len()))

		// Parse response into ResponseBody or unmarshalled Result
		err = r.Unmarshal(resp.ResponseBody)
		r.Stage("Unmarshal")
	}

	r.Complete()
	return
}

func (r *Request) UnmarshalCache(b *bytes.Buffer) (*CacheEntry, error) {
	var c *CacheEntry
	if err := json.Unmarshal(b.Bytes(), &c); err != nil {
		return nil, err
	}

	return c, nil
}

func (r *Request) Unmarshal(b *bytes.Buffer) error {
	if r.Result != nil {
		return json.Unmarshal(b.Bytes(), r.Result)
	} else {
		r.ResponseBody = b
	}

	return nil
}

func (r *Request) MarshalCache() ([]byte, error) {
	c := CacheEntry{
		Status:     r.ResponseStatus,
		StatusCode: r.ResponseStatusCode,
		Header:     r.ResponseHeader,
		Body:       r.ResponseBody.Bytes(),
	}
	return json.Marshal(&c)
}

func (r *Request) Marshal() ([]byte, error) {
	if r.Result != nil {
		return json.Marshal(r.Result)
	} else if r.ResponseBody != nil {
		return r.ResponseBody.Bytes(), nil
	}

	return nil, errors.New("No data available")
}

func (r *Request) ApplyResponse(resp *napping.Response) {
	if resp == nil {
		return
	}

	r.ResponseStatusCode = resp.Status()
	r.ResponseStatus = resp.HttpResponse().Status
	r.ResponseHeader = resp.HttpResponse().Header
	r.ResponseBody = resp.ResponseBody
	r.Size(uint64(resp.ResponseBody.Len()))
}

func (r *Request) ApplyCache(c *CacheEntry) {
	if c == nil {
		return
	}

	r.ResponseStatusCode = c.StatusCode
	r.ResponseStatus = c.Status
	r.ResponseHeader = c.Header
	r.ResponseBody = bytes.NewBuffer(c.Body)
	r.Size(uint64(len(c.Body)))
}

func (r *Request) String() string {
	if r.IsComplete() {
		r.Complete()
	}

	params, _ := url.QueryUnescape(r.Params.Encode())
	return fmt.Sprintf(`Trace for request: %s
               URL: %s %s
            Params: %s
            Header: %+v
           Payload: %s
%s

             Error: %#v
              Size: %s
            Status: %s
        StatusCode: %d
   Response Header: %+v
	`, r.Description, r.Method, r.URL,
		params, r.Header, string(r.originalPayload), r.Tracer.String(),
		r.ResponseError, humanize.Bytes(r.ResponseSize), r.ResponseStatus, r.ResponseStatusCode, r.ResponseHeader)
}

func (r *Request) Reset() {
	r.Tracer.Reset()

	r.ResponseSize = 0
	r.ResponseBody = nil
	r.ResponseError = nil
}

func (r *Request) Size(size uint64) {
	r.ResponseSize = size
}

func (r *Request) Error(err error) {
	if err != nil {
		r.ResponseError = err
	}
}

func (r *Request) LogStatus() {
	if config.Args.EnableRequestTracing {
		log.Debugf(r.String())
	}
}
