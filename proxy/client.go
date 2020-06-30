package proxy

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/elgatito/elementum/config"
)

var (
	dialer = &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 15 * time.Second,
		DualStack: true,
	}

	// InternalProxyURL holds parsed internal proxy url
	internalProxyURL, _ = url.Parse("http://127.0.0.1:65222")

	directTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext:     CustomDialContext,
	}
	directClient = &http.Client{
		Transport: directTransport,
		Timeout:   15 * time.Second,
	}

	proxyTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           http.ProxyURL(internalProxyURL),
	}
	proxyClient = &http.Client{
		Transport: proxyTransport,
		Timeout:   30 * time.Second,
	}
)

// Reload ...
func Reload() {
	if config.Get().ProxyURL == "" || !config.Get().ProxyUseHTTP {
		directTransport.Proxy = nil
	} else {
		proxyURL, _ := url.Parse(config.Get().ProxyURL)
		directTransport.Proxy = GetProxyURL(proxyURL)

		log.Debugf("Setting up proxy for direct client: %s", config.Get().ProxyURL)
	}
}

// GetClient ...
func GetClient() *http.Client {
	if !config.Get().InternalProxyEnabled {
		return directClient
	}

	return proxyClient
}

// GetDirectClient ...
func GetDirectClient() *http.Client {
	return directClient
}

// CustomDial ...
func CustomDial(network, addr string) (net.Conn, error) {
	if !config.Get().InternalDNSEnabled {
		return dialer.Dial(network, addr)
	}

	addrs := strings.Split(addr, ":")
	if len(addrs) == 2 && len(addrs[0]) > 2 && strings.Index(addrs[0], ".") > -1 {
		if ipTest := net.ParseIP(addrs[0]); ipTest == nil {
			if ips, err := resolve(addrs[0]); err == nil && len(ips) > 0 {
				for _, i := range ips {
					if config.Get().InternalDNSSkipIPv6 {
						if ip := net.ParseIP(i); ip.To4() == nil {
							continue
						}
					}

					if c, err := dialer.Dial(network, i+":"+addrs[1]); err == nil {
						return c, err
					}
				}
			}
		}
	}

	return dialer.Dial(network, addr)
}

// CustomDialContext ...
func CustomDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !config.Get().InternalDNSEnabled {
		return dialer.DialContext(ctx, network, addr)
	}

	addrs := strings.Split(addr, ":")
	if len(addrs) == 2 && len(addrs[0]) > 2 && strings.Index(addrs[0], ".") > -1 {
		if ipTest := net.ParseIP(addrs[0]); ipTest == nil {
			if ip, err := resolve(addrs[0]); err == nil && len(ip) > 0 {
				if !config.Get().InternalDNSSkipIPv6 {
					addr = ip[0] + ":" + addrs[1]
				} else {
					for _, i := range ip {
						if len(i) == net.IPv4len {
							addr = i + ":" + addrs[1]
							break
						}
					}
				}
			}
		}
	}

	return dialer.DialContext(ctx, network, addr)
}

// GetProxyURL ...
func GetProxyURL(fixedURL *url.URL) func(*http.Request) (*url.URL, error) {
	return func(r *http.Request) (*url.URL, error) {
		if config.Get().AntizapretEnabled {
			if proxy, err := PacParser.FindProxy(r.URL.String()); err == nil && proxy != "" {
				if u, err := url.Parse(proxy); err == nil && u != nil {
					return u, nil
				}
			}
		}

		return fixedURL, nil
	}
}
