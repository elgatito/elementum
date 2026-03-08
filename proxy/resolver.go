package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnshttp"
)

const (
	defaultDoHServer   = "https://dns.quad9.net/dns-query"
	defaultDNSCacheTTL = 300
)

type DoHResolver struct {
	endpoint string
	client   *http.Client
}

type OpenNICResolver struct {
	servers []string
	client  *dns.Client
}

func NewDoHResolver(server string) *DoHResolver {
	return &DoHResolver{
		endpoint: normalizeDoHEndpoint(server),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func NewOpenNICResolver(servers []string) *OpenNICResolver {
	cleanServers := make([]string, 0, len(servers))
	seen := make(map[string]struct{}, len(servers))
	for _, s := range servers {
		resolverAddr, ok := ensureResolverAddress(s)
		if !ok {
			continue
		}
		if _, exists := seen[resolverAddr]; exists {
			continue
		}

		seen[resolverAddr] = struct{}{}
		cleanServers = append(cleanServers, resolverAddr)
	}

	return &OpenNICResolver{
		servers: cleanServers,
		client:  dns.NewClient(),
	}
}

func (c *DoHResolver) Resolve(ctx context.Context, host string) ([]string, int, error) {
	m := dns.NewMsg(host, dns.TypeA)
	if m == nil {
		return nil, 0, fmt.Errorf("invalid DNS query type for host %s", host)
	}

	req, err := dnshttp.NewRequest(http.MethodPost, c.endpoint, m)
	if err != nil {
		return nil, 0, err
	}

	req = req.WithContext(ctx)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()

		return nil, 0, fmt.Errorf("doh: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	dnsResp, err := dnshttp.Response(resp)
	if err != nil {
		return nil, 0, err
	}
	if dnsResp == nil {
		return nil, 0, fmt.Errorf("doh: empty response")
	}
	if dnsResp.Rcode != dns.RcodeSuccess {
		return nil, 0, fmt.Errorf("doh: rcode=%d", dnsResp.Rcode)
	}

	ips, ttl := extractIPsAndTTL(dnsResp)
	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("doh: no A/AAAA answers")
	}

	return ips, ttl, nil
}

func (c *OpenNICResolver) Resolve(ctx context.Context, host string) ([]string, int, error) {
	if len(c.servers) == 0 {
		return nil, 0, fmt.Errorf("opennic: no resolvers configured")
	}

	var lastErr error
	for _, server := range c.servers {
		m := dns.NewMsg(host, dns.TypeA)
		if m == nil {
			return nil, 0, fmt.Errorf("invalid DNS query type for host %s", host)
		}

		resp, _, err := c.client.Exchange(ctx, m, "udp", server)
		if err != nil {
			lastErr = fmt.Errorf("opennic resolver %s: %w", server, err)
			continue
		}
		if resp == nil {
			lastErr = fmt.Errorf("opennic resolver %s: empty response", server)
			continue
		}
		if resp.Rcode != dns.RcodeSuccess {
			lastErr = fmt.Errorf("opennic resolver %s: rcode=%d", server, resp.Rcode)
			continue
		}

		ips, ttl := extractIPsAndTTL(resp)
		if len(ips) == 0 {
			lastErr = fmt.Errorf("opennic resolver %s: no A/AAAA answers", server)
			continue
		}

		return ips, ttl, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("opennic: all resolvers failed")
	}

	return nil, 0, lastErr
}

func normalizeDoHEndpoint(server string) string {
	server = strings.TrimSpace(server)
	if server == "" {
		server = defaultDoHServer
	}
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}

	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return defaultDoHServer
	}

	u.RawQuery = ""
	u.Fragment = ""
	u.Path = trimDoHPath(u.Path)

	return strings.TrimRight(u.String(), "/")
}

func trimDoHPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return ""
	}

	path = strings.TrimRight(path, "/")
	path = strings.TrimSuffix(path, dnshttp.Path)

	if path == "/" {
		return ""
	}

	return path
}

func extractIPsAndTTL(resp *dns.Msg) ([]string, int) {
	if resp == nil {
		return nil, defaultDNSCacheTTL
	}

	ips := make([]string, 0, len(resp.Answer))
	ttl := 0

	for _, rr := range resp.Answer {
		var ip netip.Addr

		switch v := rr.(type) {
		case *dns.A:
			ip = v.Addr
		case *dns.AAAA:
			ip = v.Addr
		default:
			continue
		}

		if !ip.IsValid() {
			continue
		}

		ips = append(ips, ip.String())

		recordTTL := int(rr.Header().TTL)
		if recordTTL > 0 && (ttl == 0 || recordTTL < ttl) {
			ttl = recordTTL
		}
	}

	if ttl <= 0 {
		ttl = defaultDNSCacheTTL
	}

	return ips, ttl
}

func ensureResolverAddress(value string) (string, bool) {
	rawHost := strings.TrimSpace(value)
	if rawHost == "" {
		return "", false
	}
	dnsPort := "53"

	if host, port, err := net.SplitHostPort(rawHost); err == nil {
		rawHost = host
		if port != "" {
			dnsPort = port
		}
	}

	ip := net.ParseIP(rawHost)
	if ip == nil {
		return "", false
	}

	return net.JoinHostPort(ip.String(), dnsPort), true
}
