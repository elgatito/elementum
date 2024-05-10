package proxy

import (
	"context"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/bogdanovich/dns_resolver"
	"github.com/likexian/doh/dns"
	"github.com/likexian/gokit/xcache"

	"github.com/elgatito/elementum/config"
)

var (
	// opennicZones contains all zones from Opennic services.
	// List can be taken here: https://wiki.opennic.org/opennic/dot
	opennicZones = []string{
		"bbs",
		"chan",
		"cyb",
		"dyn",
		"geek",
		"gopher",
		"indy",
		"libre",
		"neo",
		"null",
		"o",
		"oss",
		"oz",
		"parody",
		"pirate",
		"free",
		"bazar",
		"coin",
		"emc",
		"lib",
		"fur",
		"bit",
		"ku",
		"te",
		"ti",
		"uu",
	}

	commonResolver  = &DoH{}
	opennicResolver = &dns_resolver.DnsResolver{}

	commonLock  = sync.RWMutex{}
	opennicLock = sync.RWMutex{}

	dnsCacheResults = xcache.New(xcache.MemoryCache)
	dnsRunLocks     sync.Map
)

// Supported dns response types
var (
	ResponseTypeA    = 1
	ResponseTypeAAAA = 28
)

func init() {
	reloadDNS()
}

func reloadDNS() {
	commonLock.Lock()
	opennicLock.Lock()

	defer func() {
		commonLock.Unlock()
		opennicLock.Unlock()
	}()

	commonResolver = UseProviders(GoogleProvider, CloudflareProvider, Quad9Provider)
	opennicResolver = dns_resolver.New(config.Get().InternalDNSOpenNic)

	dnsCacheResults.Flush()
}

// Each request is going through this workflow:
// Check cache -> Query Opennic (if address belongs to Opennic domains) -> Query DoH providers -> Save cache
func resolveAddr(addr string) (ret []string, err error) {
	defer perf.ScopeTimer()()

	// Check for results in the cache
	if cached := dnsCacheResults.Get(addr); cached != nil {
		return cached.([]string), nil
	}

	// Lock DNS calls for same address and wait for cache to fill in
	var mu *sync.Mutex
	if m, ok := dnsRunLocks.Load(addr); ok {
		mu = m.(*sync.Mutex)
	} else {
		mu = &sync.Mutex{}
		dnsRunLocks.Store(addr, mu)
	}

	mu.Lock()

	cacheTTL := 300

	defer func() {
		if len(ret) > 0 {
			dnsCacheResults.Set(addr, ret, int64(cacheTTL))
		}
		mu.Unlock()
	}()

	// Resolve Opennic address
	if isOpennicDomain(getZone(addr)) {
		if ips := resolveOpennicAddr(addr); len(ips) > 0 {
			ret = ips
			return
		}
	}

	commonLock.RLock()
	defer commonLock.RUnlock()

	// Resolve with common resolver using DoH
	if resp, err := commonResolver.Query(context.TODO(), dns.Domain(addr), dns.TypeA); err == nil {
		ret = IPs(resp)
		cacheTTL = TTL(resp)

		return ret, err
	} else {
		return nil, err
	}
}

func getZone(addr string) string {
	ary := strings.Split(addr, ".")
	return ary[len(ary)-1]
}

func isOpennicDomain(zone string) bool {
	for _, z := range opennicZones {
		if z == zone {
			return true
		}
	}

	return false
}

func resolveOpennicAddr(host string) (ips []string) {
	defer perf.ScopeTimer()()

	opennicLock.RLock()
	defer opennicLock.RUnlock()

	ipsResolved, err := opennicResolver.LookupHost(host)
	if err == nil && len(ipsResolved) > 0 {
		for _, i := range ipsResolved {
			ips = append(ips, i.String())
		}

		return
	}

	return
}
