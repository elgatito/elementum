package proxy

import (
	"context"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/bogdanovich/dns_resolver"
	"github.com/likexian/doh/dns"

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

	dnsCacheResults sync.Map
	dnsCacheLocks   sync.Map
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
}

func resolve(addr string) ([]string, error) {
	defer perf.ScopeTimer()()

	if isOpennicDomain(getZone(addr)) {
		if ips := resolveAddr(addr); len(ips) > 0 {
			return ips, nil
		}
	}

	// TODO: Remove if there are no synchronous hash writes panics
	// var mu *sync.Mutex
	// if m, ok := dnsCacheLocks.Load(addr); ok {
	// 	mu = m.(*sync.Mutex)
	// } else {
	// 	mu = &sync.Mutex{}
	// 	dnsCacheLocks.Store(addr, mu)
	// }

	// mu.Lock()
	// defer mu.Unlock()

	commonLock.RLock()
	defer commonLock.RUnlock()

	if resp, err := commonResolver.Query(context.TODO(), dns.Domain(addr), dns.TypeA); err == nil {
		return IPs(resp), nil
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

// This is very dump solution.
// We have a sync.Map with results for resolving IPs
// and a sync.Map with mutexes for each map.
// Mutexes are needed because torrent files are resolved concurrently and so
// DNS queries run concurrently as well, thus DNS hosts can ban for
// doing so many queries. So we wait until first one is finished.
// Possibly need to cleanup saved IPs after some time.
// Each request is going through this workflow:
// Check saved -> Query Google/Quad9 -> Check saved -> Query Opennic -> Save
func resolveAddr(host string) (ips []string) {
	if cached, ok := dnsCacheResults.Load(host); ok {
		return strings.Split(cached.(string), ",")
	}

	defer perf.ScopeTimer()()

	var mu *sync.Mutex
	if m, ok := dnsCacheLocks.Load(host); ok {
		mu = m.(*sync.Mutex)
	} else {
		mu = &sync.Mutex{}
		dnsCacheLocks.Store(host, mu)
	}

	mu.Lock()

	defer func() {
		mu.Unlock()
		if len(ips) == 0 || strings.HasPrefix(ips[0], "127.") {
			return
		}

		dnsCacheResults.Store(host, strings.Join(ips, ","))
	}()

	if cached, ok := dnsCacheResults.Load(host); ok {
		return strings.Split(cached.(string), ",")
	}

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
