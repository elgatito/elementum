package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
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
		"epic",
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
		"ko",
		"rm",
	}

	defaultOpenNICResolverServers = []string{
		"94.247.43.254",
		"152.53.15.127",
		"185.181.61.24",
	}

	commonResolver  = NewDoHResolver(defaultDoHServer)
	opennicResolver = NewOpenNICResolver(defaultOpenNICResolverServers)

	commonLock  = sync.RWMutex{}
	opennicLock = sync.RWMutex{}

	dnsCacheResults = xcache.New(xcache.MemoryCache)
	dnsRunLocks     sync.Map
)

const (
	openNICResolversAPI   = "https://api.opennicproject.org/geoip/?bare&res=3&adm=3&rnd=true&ipv=4"
	openNICRequestTimeout = 7 * time.Second
)

type resolveResult struct {
	source string
	ips    []string
	ttl    int
	err    error
}

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

	commonResolver = NewDoHResolver(config.Get().InternalDNSServer)

	if config.Get().InternalDNSOpenNicUse {
		opennicResolvers, source := fetchOpenNICResolvers()
		opennicResolver = NewOpenNICResolver(opennicResolvers)
		log.Debugf("Configured OpenNIC resolvers from %s: %+v", source, opennicResolvers)
	}

	dnsCacheResults.Flush()
}

// Each request is going through this workflow:
// Check cache -> Query DoH & Query Opennic (if enabled for everything or if address belongs to Opennic domains) -> Save cache
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
	defer mu.Unlock()

	if cached := dnsCacheResults.Get(addr); cached != nil {
		return cached.([]string), nil
	}

	zone := getZone(addr)
	isOpenNICZone := isOpennicDomain(zone)

	runDoH := !isOpenNICZone
	runOpenNIC := config.Get().InternalDNSOpenNicUse && (isOpenNICZone || !config.Get().InternalDNSOpenNicOnlySpecialZones)

	results := make(chan resolveResult, 2)
	queriesCount := 0

	if runDoH {
		queriesCount++
		go queryCommonResolver(addr, results)
	}

	if runOpenNIC {
		queriesCount++
		go queryOpenNICResolver(addr, results)
	}

	if queriesCount == 0 {
		return nil, fmt.Errorf("dns: no resolver selected for host %s", addr)
	}

	cacheTTL := 0
	seenIPs := make(map[string]struct{})
	var firstErr error

	for i := 0; i < queriesCount; i++ {
		result := <-results
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}

			log.Debugf("DNS resolver %s failed for %s: %s", result.source, addr, result.err)
			continue
		}
		log.Debugf("DNS resolver %s returned %v IPs for %s", result.source, result.ips, addr) //TODO: remove

		ips := filterResolvedIPs(result.ips, addr)
		if len(ips) == 0 {
			continue
		}

		for _, ip := range ips {
			if _, exists := seenIPs[ip]; exists {
				continue
			}

			seenIPs[ip] = struct{}{}
			ret = append(ret, ip)
		}

		if result.ttl > 0 && (cacheTTL == 0 || result.ttl < cacheTTL) {
			cacheTTL = result.ttl
		}
	}

	if len(ret) == 0 {
		if firstErr == nil {
			firstErr = fmt.Errorf("dns: all resolvers failed for %s", addr)
		}

		return nil, firstErr
	}
	if cacheTTL <= 0 {
		cacheTTL = defaultDNSCacheTTL
	}

	dnsCacheResults.Set(addr, ret, int64(cacheTTL))

	return ret, nil
}

func getZone(addr string) string {
	addr = strings.TrimSpace(strings.TrimSuffix(addr, "."))
	if addr == "" {
		return ""
	}

	ary := strings.Split(addr, ".")
	return strings.ToLower(ary[len(ary)-1])
}

func isOpennicDomain(zone string) bool {
	for _, z := range opennicZones {
		if z == zone {
			return true
		}
	}

	return false
}

func queryCommonResolver(host string, ch chan<- resolveResult) {
	defer perf.ScopeTimer()()

	commonLock.RLock()
	resolver := commonResolver
	commonLock.RUnlock()

	if resolver == nil {
		ch <- resolveResult{source: "doh", err: fmt.Errorf("dns: common resolver is not configured")}
		return
	}

	ips, ttl, err := resolver.Resolve(context.Background(), host)
	ch <- resolveResult{
		source: "doh",
		ips:    ips,
		ttl:    ttl,
		err:    err,
	}
}

func queryOpenNICResolver(host string, ch chan<- resolveResult) {
	defer perf.ScopeTimer()()

	opennicLock.RLock()
	resolver := opennicResolver
	opennicLock.RUnlock()

	if resolver == nil {
		ch <- resolveResult{source: "opennic", err: fmt.Errorf("dns: OpenNIC resolver is not configured")}
		return
	}

	ips, ttl, err := resolver.Resolve(context.Background(), host)
	ch <- resolveResult{
		source: "opennic",
		ips:    ips,
		ttl:    ttl,
		err:    err,
	}
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

	for line := range strings.SplitSeq(body, "\n") {
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
