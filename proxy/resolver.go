package proxy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/likexian/doh/dns"
	"github.com/likexian/doh/provider/cloudflare"
	"github.com/likexian/doh/provider/dnspod"
	"github.com/likexian/doh/provider/google"
	"github.com/likexian/doh/provider/quad9"
	"github.com/likexian/gokit/xcache"
	"github.com/likexian/gokit/xhash"
)

// Slightly modified DNS resolver, taken from github.com/likexian/doh/dns
// Differences:
// 		- Does not store stats, so always will query all providers and return fastest response.
// 		- Checks whether response points to localhost (127.0.0.1) to ignore that response.

// provider is provider
type provider uint

// Provider is the provider interface
type Provider interface {
	Query(context.Context, dns.Domain, dns.Type, ...dns.ECS) (*dns.Response, error)
	String() string
}

// DoH is doh client
type DoH struct {
	providers []Provider
	cache     xcache.Cachex
	stopc     chan bool
	sync.RWMutex
}

// DoH Providers enum
const (
	CloudflareProvider provider = iota
	DNSPodProvider
	GoogleProvider
	Quad9Provider
)

// DoH Providers list
var (
	Providers = []provider{
		CloudflareProvider,
		DNSPodProvider,
		GoogleProvider,
		Quad9Provider,
	}
)

// New returns a new DoH client, quad9 is default
func New(provider provider) Provider {
	switch provider {
	case CloudflareProvider:
		return cloudflare.NewClient()
	case DNSPodProvider:
		return dnspod.NewClient()
	case GoogleProvider:
		return google.NewClient()
	default:
		return quad9.NewClient()
	}
}

func UseProviders(provider ...provider) *DoH {
	c := &DoH{
		providers: []Provider{},
		cache:     xcache.New(xcache.MemoryCache),
		stopc:     make(chan bool),
	}

	if len(provider) == 0 {
		provider = Providers
	}

	for _, v := range provider {
		c.providers = append(c.providers, New(v))
	}

	return c
}

// Close close doh client
func (c *DoH) Close() {
	c.stopc <- true
	if c.cache != nil {
		c.cache.Close()
	}
}

// Query do DoH query
func (c *DoH) Query(ctx context.Context, d dns.Domain, t dns.Type, s ...dns.ECS) (*dns.Response, error) {
	cacheKey := ""
	if c.cache != nil {
		var ss string
		if len(s) > 0 && s[0] != "" {
			ss = strings.TrimSpace(string(s[0]))
		}
		cacheKey = xhash.Sha1(string(d), string(t), ss).Hex()
		v := c.cache.Get(cacheKey)
		if v != nil {
			return v.(*dns.Response), nil
		}
	}

	ctxs, cancels := context.WithCancel(ctx)
	defer cancels()

	r := make(chan interface{})
	for k, p := range c.providers {
		go func(k int, p Provider) {
			rsp, err := p.Query(ctxs, d, t, s...)
			if err == nil {
				r <- rsp
			} else {
				r <- nil
			}
		}(k, p)
	}

	total := 0
	result := &dns.Response{
		Status: -1,
	}

	for v := range r {
		total++
		if v != nil {
			cancels()
			result = v.(*dns.Response)

			// Ignoring results that point to ourselves
			if ips := IPs(result); slices.Contains(ips, "127.0.0.1") {
				log.Debugf("Ignoring response - %#v", result)
				continue
			}

			if cacheKey != "" {
				ttl := 30
				if len(result.Answer) > 0 {
					ttl = result.Answer[0].TTL
				}
				_ = c.cache.Set(cacheKey, result, int64(ttl))
			}
		}
		if total >= len(c.providers) {
			close(r)
			break
		}
	}

	if result.Status == -1 {
		return nil, fmt.Errorf("doh: all query failed")
	}

	return result, nil
}

func IPs(resp *dns.Response) []string {
	if resp != nil && resp.Answer != nil {
		ips := make([]string, 0, len(resp.Answer))
		for _, a := range resp.Answer {
			ips = append(ips, a.Data)
		}
		return ips
	}

	return nil
}
