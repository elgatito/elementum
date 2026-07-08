package proxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/elgatito/elementum/config"
)

const (
	openNICResolversAPI   = "https://api.opennicproject.org/geoip/?bare&res=3&adm=3&rnd=true&ipv=4"
	openNICRequestTimeout = 7 * time.Second
)

var (
	// opennicZones contains all zones from Opennic services.
	// List can be taken here: https://wiki.opennic.org/opennic/dot
	opennicZones = []string{
		"bazar",
		"bbs",
		"bit",
		"chan",
		"coin",
		"cyb",
		"dyn",
		"emc",
		"epic",
		"free",
		"fur",
		"geek",
		"gopher",
		"indy",
		"ko",
		"ku",
		"lib",
		"libre",
		"neo",
		"null",
		"o",
		"oss",
		"oz",
		"parody",
		"pirate",
		"rm",
		"te",
		"ti",
		"uu",
	}

	defaultOpenNICResolverServers = []string{
		"94.247.43.254",
		"152.53.15.127",
		"95.216.99.249",
	}

	commonResolver  = &CustomDNS{}
	opennicResolver = &CustomDNS{}

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

	commonResolver = UseDoHProviders(GoogleProvider, CloudflareProvider, Quad9Provider)
	opennicResolver = UseDNSProviders(defaultOpenNICResolverServers...)

	if config.Get().InternalDNSOpenNICUse {
		opennicResolvers, source := fetchOpenNICResolvers()
		opennicResolver = UseDNSProviders(opennicResolvers...)
		log.Debugf("Configured OpenNIC resolvers from %s: %+v", source, opennicResolvers)
	}
}

// Each request is going through this workflow:
// Check cache -> Query Opennic (if address belongs to Opennic domains) -> Query DoH providers -> Save cache
func resolveAddr(ctx context.Context, addr string) (ret []string, err error) {
	defer perf.ScopeTimer()()

	// Resolve Opennic address
	if isOpennicDomain(getZone(addr)) {
		if ips := resolveOpennicAddr(ctx, addr); len(ips) > 0 {
			if ips = filterResolvedIPs(ips, addr); len(ips) > 0 {
				ret = ips
				return
			}
		}
	}

	commonLock.RLock()
	defer commonLock.RUnlock()

	// Resolve with common resolver using DoH
	if resp, err := commonResolver.Query(ctx, addr); err == nil {
		return IPs(resp), err
	} else {
		return nil, err
	}
}

func getZone(addr string) string {
	ary := strings.Split(addr, ".")
	if len(ary) == 0 {
		return ""
	}
	return ary[len(ary)-1]
}

func isOpennicDomain(zone string) bool {
	return slices.Contains(opennicZones, zone)
}

func resolveOpennicAddr(ctx context.Context, host string) (ips []string) {
	defer perf.ScopeTimer()()

	opennicLock.RLock()
	defer opennicLock.RUnlock()

	ipsResolved, err := opennicResolver.Query(ctx, host)
	if err == nil && len(ipsResolved) > 0 {
		for _, i := range ipsResolved {
			ips = append(ips, i.String())
		}

		return
	}

	return
}

func fetchOpenNICResolvers() ([]string, string) {
	client := &http.Client{
		Timeout: openNICRequestTimeout,
	}

	req, err := http.NewRequest(http.MethodGet, openNICResolversAPI, nil)
	if err != nil {
		log.Warningf("Could not prepare OpenNIC API request: %s", err)
		return defaultOpenNICResolverServers, "fallback"
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Warningf("Could not fetch OpenNIC resolvers, using fallback list: %s", err)
		return defaultOpenNICResolverServers, "fallback"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Warningf("OpenNIC API returned status %d, using fallback list", resp.StatusCode)
		return defaultOpenNICResolverServers, "fallback"
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		log.Warningf("Could not read OpenNIC API response, using fallback list: %s", err)
		return defaultOpenNICResolverServers, "fallback"
	}

	resolvers := parseOpenNICResolvers(string(body))
	if len(resolvers) == 0 {
		log.Warningf("OpenNIC API returned no valid resolvers, using fallback list")
		return defaultOpenNICResolverServers, "fallback"
	}

	return resolvers, "api"
}

func parseOpenNICResolvers(body string) []string {
	servers := make([]string, 0)
	seen := make(map[string]struct{})

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if ip := net.ParseIP(line); ip == nil {
			log.Debugf("Ignoring invalid OpenNIC resolver from API: %s", line)
			continue
		}

		resolverAddr := net.JoinHostPort(line, "53")
		if _, exists := seen[resolverAddr]; exists {
			continue
		}

		seen[resolverAddr] = struct{}{}
		servers = append(servers, resolverAddr)
	}

	return servers
}

func filterResolvedIPs(ips []string, host string) []string {
	filtered := make([]string, 0, len(ips))
	isLocalhost := strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(host), "."), "localhost")

	for _, ipStr := range ips {
		ip := net.ParseIP(strings.TrimSpace(ipStr))
		if ip == nil {
			continue
		}

		normalized := ip.String()
		if !isLocalhost && normalized == "127.0.0.1" {
			continue
		}

		filtered = append(filtered, normalized)
	}

	return filtered
}
