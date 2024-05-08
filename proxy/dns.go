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

func resolveAddr(addr string) ([]string, error) {
	defer perf.ScopeTimer()()

	if isOpennicDomain(getZone(addr)) {
		if ips := resolveOpennicAddr(addr); len(ips) > 0 {
			return ips, nil
		}
	}

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

// This is very dumb solution.
// Each request is going through this workflow:
// Check saved -> Query Google/Quad9 -> Check saved -> Query Opennic -> Save
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
