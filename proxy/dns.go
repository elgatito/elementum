package proxy

import (
	"context"
	"strings"
	"sync"

	"github.com/anacrolix/missinggo/perf"
	"github.com/bogdanovich/dns_resolver"
	"github.com/likexian/doh-go"
	"github.com/likexian/doh-go/dns"
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

	commonResolver  = doh.Use(doh.CloudflareProvider, doh.GoogleProvider)
	opennicResolver = dns_resolver.New([]string{"163.172.168.171", "51.89.88.77", "172.98.193.62"})

	dnsCacheResults sync.Map
	dnsCacheLocks   sync.Map
)

func init() {
	commonResolver.EnableCache(true)
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

	resp, err := commonResolver.Query(context.TODO(), dns.Domain(addr), dns.TypeA)
	if err == nil && resp != nil && resp.Answer != nil {
		ips := make([]string, 0, len(resp.Answer))
		for _, a := range resp.Answer {
			ips = append(ips, a.Data)
		}
		return ips, nil
	}

	return nil, err
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

	ipsResolved, err := opennicResolver.LookupHost(host)
	if err == nil && len(ipsResolved) > 0 {
		for _, i := range ipsResolved {
			ips = append(ips, i.String())
		}

		return
	}

	return
}
