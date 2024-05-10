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

	r := make(chan *dns.Response, len(c.providers))

	result := &dns.Response{
		Status: -1,
	}

	var wg sync.WaitGroup
	for _, p := range c.providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()

			resp, err := p.Query(ctxs, d, t, s...)
			if err != nil {
				return
			}

			// Ignoring results that point to ourselves, unless we want to resolve localhost
			if ips := IPs(resp); d != "localhost" && slices.Contains(ips, "127.0.0.1") {
				return
			}

			cancels()

			if cacheKey != "" {
				ttl := 30
				if len(result.Answer) > 0 {
					ttl = result.Answer[0].TTL
				}
				_ = c.cache.Set(cacheKey, resp, int64(ttl))
			}

			r <- resp
		}(p)
	}

	go func() {
		wg.Wait()
		close(r)
	}()

	result = <-r

	if result == nil || result.Status == -1 {
		return nil, fmt.Errorf("doh: all query failed")
	}

	return result, nil
}

func IPs(resp *dns.Response) []string {
	if resp != nil && resp.Answer != nil {
		ips := make([]string, 0, len(resp.Answer))
		for _, a := range resp.Answer {
			if a.Type != ResponseTypeA && a.Type != ResponseTypeAAAA {
				continue
			}
			ips = append(ips, a.Data)
		}
		return ips
	}

	return nil
}
