package proxy

import (
	"context"
	"net"
	"testing"
)

func TestGetZone(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{name: "simple domain", addr: "example.com", expected: "com"},
		{name: "subdomain", addr: "sub.example.com", expected: "com"},
		{name: "opennic bbs", addr: "myhost.bbs", expected: "bbs"},
		{name: "opennic chan", addr: "irc.chan", expected: "chan"},
		{name: "opennic pirate", addr: "seed.pirate", expected: "pirate"},
		{name: "opennic geek", addr: "wiki.geek", expected: "geek"},
		{name: "opennic null", addr: "dev.null", expected: "null"},
		{name: "single label", addr: "localhost", expected: "localhost"},
		{name: "multi-level subdomain", addr: "a.b.c.example.com", expected: "com"},
		{name: "opennic lib", addr: "books.lib", expected: "lib"},
		{name: "opennic cyb", addr: "node.cyb", expected: "cyb"},
		{name: "opennic epic", addr: "game.epic", expected: "epic"},
		{name: "opennic o", addr: "short.o", expected: "o"},
		{name: "opennic fur", addr: "pets.fur", expected: "fur"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getZone(tt.addr)
			if result != tt.expected {
				t.Errorf("getZone(%q) = %q, want %q", tt.addr, result, tt.expected)
			}
		})
	}
}

func TestIsOpennicDomain(t *testing.T) {
	opennicTests := []string{
		"bbs", "chan", "cyb", "dyn", "epic", "geek", "gopher",
		"indy", "libre", "neo", "null", "o", "oss", "oz",
		"parody", "pirate", "free", "bazar", "coin", "emc",
		"lib", "fur", "bit", "ku", "te", "ti", "uu", "ko", "rm",
	}

	for _, zone := range opennicTests {
		t.Run("opennic_"+zone, func(t *testing.T) {
			if !isOpennicDomain(zone) {
				t.Errorf("isOpennicDomain(%q) = false, want true", zone)
			}
		})
	}

	nonOpennicTests := []string{
		"com", "org", "net", "io", "dev", "xyz", "uk", "de",
		"fr", "jp", "ru", "br", "au", "ca", "it", "es", "nl",
	}

	for _, zone := range nonOpennicTests {
		t.Run("non_opennic_"+zone, func(t *testing.T) {
			if isOpennicDomain(zone) {
				t.Errorf("isOpennicDomain(%q) = true, want false", zone)
			}
		})
	}
}

func TestIPs(t *testing.T) {
	tests := []struct {
		name     string
		input    []net.IPAddr
		expected []string
	}{
		{
			name:     "empty",
			input:    []net.IPAddr{},
			expected: []string{},
		},
		{
			name: "single ipv4",
			input: []net.IPAddr{
				{IP: net.ParseIP("192.168.1.1")},
			},
			expected: []string{"192.168.1.1"},
		},
		{
			name: "single ipv6",
			input: []net.IPAddr{
				{IP: net.ParseIP("::1")},
			},
			expected: []string{"::1"},
		},
		{
			name: "multiple ips",
			input: []net.IPAddr{
				{IP: net.ParseIP("8.8.8.8")},
				{IP: net.ParseIP("8.8.4.4")},
				{IP: net.ParseIP("1.1.1.1")},
			},
			expected: []string{"8.8.8.8", "8.8.4.4", "1.1.1.1"},
		},
		{
			name: "mixed ipv4 ipv6",
			input: []net.IPAddr{
				{IP: net.ParseIP("192.168.0.1")},
				{IP: net.ParseIP("2001:db8::1")},
			},
			expected: []string{"192.168.0.1", "2001:db8::1"},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IPs(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("IPs() len = %d, want %d", len(result), len(tt.expected))
				return
			}
			for i, ip := range result {
				if ip != tt.expected[i] {
					t.Errorf("IPs()[%d] = %q, want %q", i, ip, tt.expected[i])
				}
			}
		})
	}
}

// TestResolveOpennicAddr_NoProviders tests that resolution fails gracefully
// when the OpenNIC resolver has no providers configured.
func TestResolveOpennicAddr_NoProviders(t *testing.T) {
	ctx := context.Background()

	opennicLock.Lock()
	originalResolver := opennicResolver
	opennicResolver = &CustomDNS{providers: nil}
	opennicLock.Unlock()

	defer func() {
		opennicLock.Lock()
		opennicResolver = originalResolver
		opennicLock.Unlock()
	}()

	ips := resolveOpennicAddr(ctx, "test.bbs")
	if len(ips) != 0 {
		t.Errorf("expected empty result from resolver with no providers, got %v", ips)
	}
}

// TestResolveAddr_NonOpennic_NoProviders tests that resolution of a non-OpenNIC
// domain fails gracefully when no DoH providers are available.
func TestResolveAddr_NonOpennic_NoProviders(t *testing.T) {
	ctx := context.Background()

	commonLock.Lock()
	originalResolver := commonResolver
	commonResolver = &CustomDNS{providers: nil}
	commonLock.Unlock()

	defer func() {
		commonLock.Lock()
		commonResolver = originalResolver
		commonLock.Unlock()
	}()

	ips, err := resolveAddr(ctx, "example.com")
	if err == nil {
		t.Errorf("expected error from resolver with no providers, got %v", ips)
	}
	if len(ips) != 0 {
		t.Errorf("expected empty result, got %v", ips)
	}
}

// TestResolveAddr_OpennicDomain_NoProviders tests that OpenNIC domain
// resolution falls through to common resolver when OpenNIC returns nothing.
func TestResolveAddr_OpennicDomain_NoProviders(t *testing.T) {
	ctx := context.Background()

	commonLock.Lock()
	originalCommon := commonResolver
	commonResolver = &CustomDNS{providers: nil}
	commonLock.Unlock()

	opennicLock.Lock()
	originalOpennic := opennicResolver
	opennicResolver = &CustomDNS{providers: nil}
	opennicLock.Unlock()

	defer func() {
		commonLock.Lock()
		commonResolver = originalCommon
		commonLock.Unlock()

		opennicLock.Lock()
		opennicResolver = originalOpennic
		opennicLock.Unlock()
	}()

	ips, err := resolveAddr(ctx, "test.bbs")
	if err == nil {
		t.Errorf("expected error when both resolvers have no providers, got %v", ips)
	}
	if len(ips) != 0 {
		t.Errorf("expected empty result, got %v", ips)
	}
}

// TestGetZone_EdgeCases tests edge cases for getZone.
func TestGetZone_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{name: "single char tld", addr: "x.y", expected: "y"},
		{name: "numeric tld-like", addr: "a.123", expected: "123"},
		{name: "long tld", addr: "site.photography", expected: "photography"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getZone(tt.addr)
			if result != tt.expected {
				t.Errorf("getZone(%q) = %q, want %q", tt.addr, result, tt.expected)
			}
		})
	}
}

// TestResolveAddr_Default tests that resolution of an usual domains works correctly.
func TestResolveAddr_Default(t *testing.T) {
	ctx := context.Background()

	reloadDNS()

	tests := []struct {
		addr string
	}{
		{addr: "api.themoviedb.org"},
		{addr: "api.trakt.tv"},
		{addr: "webservice.fanart.tv"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			ips, err := resolveAddr(ctx, tt.addr)
			if err != nil {
				t.Errorf("expected no error from resolver, got %v", err)
			}
			if len(ips) == 0 {
				t.Errorf("expected non-empty result, got %v results for %s", len(ips), tt.addr)
			}
		})
	}
}

// TestResolveAddr_Opennic tests that resolution of an OpenNIC domain works correctly.
func TestResolveAddr_Opennic(t *testing.T) {
	ctx := context.Background()

	reloadDNS()

	tests := []struct {
		addr string
	}{
		{addr: "be.libre"},
		{addr: "rutor.lib"},
		{addr: "rutracker.lib"},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			ips, err := resolveAddr(ctx, tt.addr)
			if err != nil {
				t.Errorf("expected no error from resolver, got %v", err)
			}
			if len(ips) == 0 {
				t.Errorf("expected non-empty result, got %v results for %s", len(ips), tt.addr)
			}
		})
	}
}
