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
		// TODO: with 15 i get timeouts for many Trakt and TMDB,
		// with 60 i get timeouts for long calls.
		// reqapi client did not have a timeout, so maybe this client also should not have it?
		Timeout: 120 * time.Second,
	}

	proxyTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           http.ProxyURL(internalProxyURL),
	}
	proxyClient = &http.Client{
		Transport: proxyTransport,
		Timeout:   120 * time.Second,
	}
)

// Reload ...
func Reload() {
	reloadDNS()

	if config.Get().ProxyURL == "" || !config.Get().ProxyUseHTTP {
		directTransport.Proxy = nil
	} else {
		proxyURL, _ := url.Parse(config.Get().ProxyURL)
		directTransport.Proxy = http.ProxyURL(proxyURL)

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
	if len(addrs) == 2 && len(addrs[0]) > 2 && strings.Contains(addrs[0], ".") {
		if ipTest := net.ParseIP(addrs[0]); ipTest == nil {
			log.Debugf("Resolving %s", addrs[0])
			if ips, err := resolve(addrs[0]); err == nil && len(ips) > 0 {
				log.Debugf("Resolved %s to %s", addrs[0], ips)
				for _, i := range ips {
					if config.Get().InternalDNSSkipIPv6 {
						if ip := net.ParseIP(i); ip == nil || ip.To4() == nil {
							continue
						}
					}

					if c, err := dialer.Dial(network, i+":"+addrs[1]); err == nil {
						return c, err
					}
				}
			} else {
				log.Debugf("Failed to resolve %s: %s", addrs[0], err)
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
	if len(addrs) == 2 && len(addrs[0]) > 2 && strings.Contains(addrs[0], ".") {
		if ipTest := net.ParseIP(addrs[0]); ipTest == nil {
			// NOTE: these changes were ported from CustomDial and they works.
			log.Debugf("Resolving %s", addrs[0])
			if ips, err := resolve(addrs[0]); err == nil && len(ips) > 0 {
				log.Debugf("Resolved %s to %s", addrs[0], ips)
				for _, i := range ips {
					if config.Get().InternalDNSSkipIPv6 {
						if ip := net.ParseIP(i); ip == nil || ip.To4() == nil {
							continue
						}
					}

					if c, err := dialer.Dial(network, i+":"+addrs[1]); err == nil {
						return c, err
					}
				}
			} else {
				log.Debugf("Failed to resolved %s: %s", addrs[0], err)
			}
		}
	}

	return dialer.DialContext(ctx, network, addr)
}
