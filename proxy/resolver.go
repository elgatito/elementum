package proxy

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	godns "github.com/ncruces/go-dns"
)

// dohprovider is provider identifier
type dohprovider uint

// Provider is the provider interface
type Provider interface {
	Query(context.Context, string) ([]net.IPAddr, error)
	String() string
}

type CustomDNSProvider struct {
	*net.Resolver
	URI string
}

// CustomDNS is custom DNS client
type CustomDNS struct {
	providers []CustomDNSProvider
}

// CustomDNS Providers enum
const (
	AllProviders dohprovider = iota
	CloudflareProvider
	GoogleProvider
	Quad9Provider
	OpenNameServerProvider
	CustomProvider
)

// Default DoH Providers list
var (
	DoHProviders = []dohprovider{
		CloudflareProvider,
		GoogleProvider,
		Quad9Provider,
		OpenNameServerProvider,
	}
)

// NewDoHProvider returns a new DoH client, quad9 is default
func NewDoHProvider(provider dohprovider) (uri string, p *net.Resolver, err error) {
	switch provider {
	case CustomProvider:
		if customDoHServer == "" {
			return "", nil, fmt.Errorf("doh: custom provider is not configured")
		}
		if strings.Contains(customDoHServer, "://") {
			uri = customDoHServer
		} else {
			uri = fmt.Sprintf("https://%s/dns-query", customDoHServer)
		}
	case CloudflareProvider:
		uri = "https://cloudflare-dns.com/dns-query"
	case GoogleProvider:
		uri = "https://dns.google/dns-query{?dns}"
	case OpenNameServerProvider:
		uri = "https://ns4.opennameserver.org/dns-query/{?dns}"
	case Quad9Provider:
		uri = "https://dns.quad9.net/dns-query"
	}

	p, err = godns.NewDoHResolver(uri)
	return uri, p, nil
}

// NewDNSProvider returns a new DNS client, local resolv is default
func NewDNSProvider(provider string) (uri string, p *net.Resolver, err error) {
	p = &net.Resolver{
		PreferGo: true, // Use Go's pure-Go DNS resolver
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, fmt.Sprintf("%s:53", provider))
		},
	}

	return provider, p, nil
}

func UseDoHProviders(provider ...dohprovider) *CustomDNS {
	c := &CustomDNS{
		providers: []CustomDNSProvider{},
	}

	if len(provider) == 0 {
		provider = DoHProviders
	}

	for _, v := range provider {
		uri, p, err := NewDoHProvider(v)
		if err != nil {
			log.Errorf("Failed to create DoH provider %d: %s", v, err)
			continue
		}

		c.providers = append(c.providers, CustomDNSProvider{
			Resolver: godns.NewCachingResolver(p),
			URI:      uri,
		})
	}

	return c
}

func UseDNSProviders(provider ...string) *CustomDNS {
	c := &CustomDNS{
		providers: []CustomDNSProvider{},
	}

	if len(provider) == 0 {
		provider = []string{"localhost"}
	}

	for _, v := range provider {
		uri, p, err := NewDNSProvider(v)
		if err != nil {
			log.Errorf("Failed to create DNS provider %s: %s", v, err)
			continue
		}

		c.providers = append(c.providers, CustomDNSProvider{
			Resolver: godns.NewCachingResolver(p),
			URI:      uri,
		})
	}

	return c
}

// Query do DoH query
func (c *CustomDNS) Query(ctx context.Context, d string) ([]net.IPAddr, error) {
	ctxs, cancels := context.WithCancel(ctx)
	defer cancels()

	r := make(chan []net.IPAddr, len(c.providers))

	var result []net.IPAddr

	var wg sync.WaitGroup
	for _, p := range c.providers {
		wg.Add(1)
		go func(p CustomDNSProvider) {
			defer wg.Done()

			resp, err := p.LookupIPAddr(ctxs, string(d))
			if err != nil {
				return
			}

			// Ignoring results that point to ourselves, unless we want to resolve localhost
			if ips := IPs(resp); d != "localhost" && slices.Contains(ips, "127.0.0.1") {
				return
			}

			cancels()

			r <- resp
		}(p)
	}

	go func() {
		wg.Wait()
		close(r)
	}()

	result = <-r

	if len(result) == 0 {
		return nil, fmt.Errorf("doh: all query failed")
	}

	return result, nil
}

func IPs(ips []net.IPAddr) []string {
	ret := make([]string, 0, len(ips))
	for _, a := range ips {
		ret = append(ret, a.String())
	}
	return ret
}
