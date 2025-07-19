package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
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
	internalProxyURL, _ = url.Parse(fmt.Sprintf("http://%s:%d", "127.0.0.1", ProxyPort))

	directTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext:     CustomDialContext,
	}
	directClient = &http.Client{
		Transport: directTransport,
	}

	proxyTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		Proxy:           http.ProxyURL(internalProxyURL),
	}
	proxyClient = &http.Client{
		Transport: proxyTransport,
	}
)

// Reload ...
func Reload() {
	reloadDNS()

	directTransport.Proxy = nil
	if config.Get().ProxyUseHTTP {
		if config.Get().ProxyURL != "" {
			proxyURL, _ := url.Parse(config.Get().ProxyURL)
			directTransport.Proxy = http.ProxyURL(proxyURL)

			log.Debugf("Setting up proxy for direct client: %s", config.Get().ProxyURL)
		} else if config.Get().AntizapretEnabled {
			go antizapretProxy.Update()
			directTransport.Proxy = antizapretProxy.ProxyURL

			log.Debugf("Setting up proxy for direct client to use Antizapret proxy dynamically.")
		}
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

// CustomDialContext ...
func CustomDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if !config.Get().InternalDNSEnabled {
		return dialer.DialContext(ctx, network, addr)
	}

	addrs := strings.Split(addr, ":")
	if len(addrs) == 2 && len(addrs[0]) > 2 && strings.Contains(addrs[0], ".") {
		if ipTest := net.ParseIP(addrs[0]); ipTest == nil {
			if ips, err := resolveAddr(addrs[0]); err == nil && len(ips) > 0 {
				for _, i := range ips {
					if config.Get().InternalDNSSkipIPv6 {
						if ip := net.ParseIP(i); ip == nil || ip.To4() == nil {
							continue
						}
					}

					if c, err := dialer.DialContext(ctx, network, i+":"+addrs[1]); err == nil {
						return c, err
					}
				}
			} else {
				log.Errorf("Failed to resolve %s: %s", addrs[0], err)
			}
		}
	}

	return dialer.DialContext(ctx, network, addr)
}
